// Command buildresources emits the fixed DK-Drive amd64 Windows resources.
// Format: https://learn.microsoft.com/en-us/windows/win32/debug/pe-format
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/danhk0612/DK-Drive/internal/app"
	"github.com/danhk0612/DK-Drive/internal/branding"
)

var le = binary.LittleEndian

func word(b *[]byte, v uint16)  { *b = le.AppendUint16(*b, v) }
func dword(b *[]byte, v uint32) { *b = le.AppendUint32(*b, v) }
func align(b *[]byte) {
	for len(*b)%4 != 0 {
		*b = append(*b, 0)
	}
}
func wide(s string) []byte {
	var b []byte
	for _, v := range utf16.Encode([]rune(s + "\x00")) {
		word(&b, v)
	}
	return b
}
func block(key string, text bool, value []byte, children ...[]byte) []byte {
	b := make([]byte, 6)
	n := len(value)
	if text {
		n /= 2
		le.PutUint16(b[4:], 1)
	}
	le.PutUint16(b[2:], uint16(n))
	b = append(b, wide(key)...)
	align(&b)
	b = append(b, value...)
	for _, child := range children {
		align(&b)
		b = append(b, child...)
	}
	le.PutUint16(b, uint16(len(b)))
	return b
}
func version() []byte {
	parts := strings.Split(app.Version, ".")
	if len(parts) != 3 {
		panic("expected major.minor.patch")
	}
	var v [4]uint32
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			panic(err)
		}
		v[i] = uint32(n)
	}
	var fixed []byte
	for _, n := range []uint32{0xfeef04bd, 0x10000, v[0]<<16 | v[1], v[2] << 16, v[0]<<16 | v[1], v[2] << 16, 0x3f, 0, 0x40004, 1, 0, 0, 0} {
		dword(&fixed, n)
	}
	var fields [][]byte
	for _, pair := range [][2]string{{"CompanyName", app.Creator}, {"FileDescription", "DK-Drive"}, {"FileVersion", app.Version + ".0"}, {"InternalName", "dkdrive"}, {"OriginalFilename", "dkdrive.exe"}, {"ProductName", "DK-Drive"}, {"ProductVersion", app.Version}} {
		fields = append(fields, block(pair[0], true, wide(pair[1])))
	}
	var translation []byte
	word(&translation, 0x409)
	word(&translation, 1200)
	return block("VS_VERSION_INFO", false, fixed, block("StringFileInfo", true, nil, block("040904B0", true, nil, fields...)), block("VarFileInfo", true, nil, block("Translation", false, translation)))
}
func icon(size int) []byte {
	mask, pixels := branding.IconBits(size)
	b := make([]byte, 40)
	le.PutUint32(b, 40)
	le.PutUint32(b[4:], uint32(size))
	le.PutUint32(b[8:], uint32(size*2))
	le.PutUint16(b[12:], 1)
	le.PutUint16(b[14:], 32)
	b = append(b, pixels...)
	// DIB mask rows use DWORD alignment; CreateIcon uses WORD alignment.
	stride := ((size + 15) / 16) * 2
	dibStride := ((size + 31) / 32) * 4
	for y := 0; y < size; y++ {
		b = append(b, mask[y*stride:(y+1)*stride]...)
		b = append(b, make([]byte, dibStride-stride)...)
	}
	return b
}

type resource struct {
	kind, id uint32
	data     []byte
}

func object() []byte {
	var group []byte
	word(&group, 0)
	word(&group, 1)
	word(&group, 3)
	var items []resource
	for i, size := range []int{16, 32, 48} {
		data := icon(size)
		items = append(items, resource{3, uint32(i + 1), data})
		group = append(group, byte(size), byte(size), 0, 0)
		word(&group, 1)
		word(&group, 32)
		dword(&group, uint32(len(data)))
		word(&group, uint16(i+1))
	}
	items = append(items, resource{14, 1, group}, resource{16, 1, version()})
	// Fixed resource tree: type -> numeric name -> English/Unicode language.
	section := make([]byte, 16+3*8)
	le.PutUint16(section[14:], 3)
	var relocs []uint32
	index := 0
	for typeIndex, kind := range []uint32{3, 14, 16} {
		count := 1
		if kind == 3 {
			count = 3
		}
		typeOffset := len(section)
		le.PutUint32(section[16+typeIndex*8:], kind)
		le.PutUint32(section[20+typeIndex*8:], uint32(typeOffset)|0x80000000)
		section = append(section, make([]byte, 16+count*8)...)
		le.PutUint16(section[typeOffset+14:], uint16(count))
		for j := 0; j < count; j++ {
			item := items[index]
			index++
			langOffset := len(section)
			le.PutUint32(section[typeOffset+16+j*8:], item.id)
			le.PutUint32(section[typeOffset+20+j*8:], uint32(langOffset)|0x80000000)
			section = append(section, make([]byte, 24)...)
			le.PutUint16(section[langOffset+14:], 1)
			le.PutUint32(section[langOffset+16:], 0x409)
			dataEntry := len(section)
			le.PutUint32(section[langOffset+20:], uint32(dataEntry))
			section = append(section, make([]byte, 16)...)
			le.PutUint32(section[dataEntry:], uint32(len(section)))
			le.PutUint32(section[dataEntry+4:], uint32(len(item.data)))
			relocs = append(relocs, uint32(dataEntry))
			section = append(section, item.data...)
			align(&section)
		}
	}
	// One .rsrc section with ADDR32NB relocations against its static symbol.
	relocationOffset := 60 + len(section)
	symbolOffset := relocationOffset + 10*len(relocs)
	b := make([]byte, 60)
	le.PutUint16(b, 0x8664)
	le.PutUint16(b[2:], 1)
	le.PutUint32(b[8:], uint32(symbolOffset))
	le.PutUint32(b[12:], 1)
	copy(b[20:], ".rsrc")
	le.PutUint32(b[36:], uint32(len(section)))
	le.PutUint32(b[40:], 60)
	le.PutUint32(b[44:], uint32(relocationOffset))
	le.PutUint16(b[52:], uint16(len(relocs)))
	le.PutUint32(b[56:], 0x40300040)
	b = append(b, section...)
	for _, offset := range relocs {
		dword(&b, offset)
		dword(&b, 0)
		word(&b, 3)
	}
	symbol := make([]byte, 18)
	copy(symbol, ".rsrc")
	le.PutUint16(symbol[12:], 1)
	symbol[16] = 3
	b = append(b, symbol...)
	dword(&b, 4)
	return b
}
func main() {
	if len(os.Args) != 2 {
		panic("usage: buildresources OUTPUT.syso")
	}
	if err := os.WriteFile(os.Args[1], object(), 0644); err != nil {
		panic(err)
	}
	fmt.Println("Windows resources:", app.Version, app.Creator)
}
