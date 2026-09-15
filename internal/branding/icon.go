package branding

// IconBits returns the existing DK-Drive bottom-up BGRA icon and transparency mask.
func IconBits(size int) ([]byte, []byte) {
	andStride := ((size + 15) / 16) * 2
	andBits := make([]byte, andStride*size)
	for i := range andBits {
		andBits[i] = 0xff
	}
	xorBits := make([]byte, size*size*4)
	set := func(x, y int, r, g, b byte) {
		row := size - 1 - y
		andBits[row*andStride+x/8] &^= 0x80 >> uint(x%8)
		i := (row*size + x) * 4
		xorBits[i], xorBits[i+1], xorBits[i+2], xorBits[i+3] = b, g, r, 0xff
	}
	for y := range size {
		for x := range size {
			sx, sy := x*32/size, y*32/size
			cx, cy := min(max(sx, 6), 25), min(max(sy, 6), 25)
			if (sx-cx)*(sx-cx)+(sy-cy)*(sy-cy) > 16 {
				continue
			}
			r, g, b := byte(18), byte(104), byte(179)
			if sx <= 3 || sx >= 28 || sy <= 3 || sy >= 28 {
				r, g, b = 11, 79, 138
			}
			set(x, y, r, g, b)
		}
	}
	for y := range size {
		for x := range size {
			sx, sy := x*32/size, y*32/size
			d := (sx >= 8 && sx <= 11 && sy >= 7 && sy <= 23) ||
				(sx >= 10 && sx <= 19 && sy >= 7 && sy <= 10) ||
				(sx >= 10 && sx <= 19 && sy >= 20 && sy <= 23) ||
				(sx >= 19 && sx <= 22 && sy >= 10 && sy <= 20)
			if d {
				set(x, y, 255, 255, 255)
			}
		}
	}
	return andBits, xorBits
}
