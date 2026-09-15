package prerequisite

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

//go:embed winfsp.json
var manifest []byte

type Installer struct {
	File   string `json:"file"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func WinFsp() Installer {
	var info Installer
	if err := json.Unmarshal(manifest, &info); err != nil {
		panic(err)
	}
	return info
}

// Verify permits execution only of the pinned, unmodified upstream installer.
func (i Installer) Verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != i.SHA256 {
		return fmt.Errorf("WinFsp 설치 파일 검증 실패: 배포 ZIP을 다시 받아 압축을 풀어주세요")
	}
	return nil
}
