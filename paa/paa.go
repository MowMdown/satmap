// Package paa reads PAA textures, the image format DayZ (and Arma) use, as
// far as satellite map tiles need: DXT1 and DXT5, stored or LZO-packed. It
// decodes the largest mipmap.
//
// A PAA is a 2-byte type (0xFF01 DXT1, 0xFF05 DXT5, ...), then tags ("GGAT"
// + a 4-byte name + a uint32 length + data), then a palette (a uint16 count
// of 3-byte colours), then the mipmaps, largest first: uint16 width (the top
// bit set when the data is LZO-packed), uint16 height, a 3-byte data size,
// the data.
package paa

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

const (
	typeDXT1 = 0xFF01
	typeDXT5 = 0xFF05
)

// Decode returns a PAA's largest mipmap (alpha is dropped: map tiles have
// none worth keeping).
func Decode(b []byte) (*image.RGBA, error) {
	if len(b) < 2 {
		return nil, errors.New("paa: too short")
	}
	typ := binary.LittleEndian.Uint16(b)
	if typ != typeDXT1 && typ != typeDXT5 {
		return nil, fmt.Errorf("paa: type %#04x isn't DXT1 or DXT5", typ)
	}
	p := 2
	for p+12 <= len(b) && string(b[p:p+4]) == "GGAT" {
		n := int(binary.LittleEndian.Uint32(b[p+8:]))
		p += 12 + n
	}
	if p+2 > len(b) {
		return nil, errors.New("paa: ends before the palette")
	}
	p += 2 + 3*int(binary.LittleEndian.Uint16(b[p:]))
	if p+7 > len(b) {
		return nil, errors.New("paa: ends before the first mipmap")
	}
	w := int(binary.LittleEndian.Uint16(b[p:]))
	h := int(binary.LittleEndian.Uint16(b[p+2:]))
	size := int(b[p+4]) | int(b[p+5])<<8 | int(b[p+6])<<16
	p += 7
	packed := w&0x8000 != 0
	w &= 0x7FFF
	if w == 0 || h == 0 || w%4 != 0 || h%4 != 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("paa: odd mipmap size %d×%d", w, h)
	}
	if p+size > len(b) {
		return nil, errors.New("paa: the mipmap runs past the end")
	}
	data := b[p : p+size]
	want := w * h // DXT5: 1 byte a pixel
	if typ == typeDXT1 {
		want /= 2
	}
	if packed {
		var err error
		if data, err = unLZO(data, want); err != nil {
			return nil, err
		}
	}
	if len(data) < want {
		return nil, errors.New("paa: the mipmap is short")
	}
	return decodeDXT(data, w, h, typ == typeDXT5), nil
}

// decodeDXT decodes DXT1 (8-byte blocks) or DXT5 (an 8-byte alpha block,
// skipped, then a DXT1 colour block) into an opaque image.
func decodeDXT(data []byte, w, h int, dxt5 bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	p := 0
	var pal [4][3]uint8
	for by := 0; by < h; by += 4 {
		for bx := 0; bx < w; bx += 4 {
			if dxt5 {
				p += 8
			}
			c0 := binary.LittleEndian.Uint16(data[p:])
			c1 := binary.LittleEndian.Uint16(data[p+2:])
			bits := binary.LittleEndian.Uint32(data[p+4:])
			p += 8
			pal[0], pal[1] = rgb565(c0), rgb565(c1)
			for i := 0; i < 3; i++ {
				a, b := int(pal[0][i]), int(pal[1][i])
				if c0 > c1 || dxt5 {
					pal[2][i] = uint8((2*a + b) / 3)
					pal[3][i] = uint8((a + 2*b) / 3)
				} else {
					pal[2][i] = uint8((a + b) / 2)
					pal[3][i] = 0
				}
			}
			for y := 0; y < 4; y++ {
				row := img.Pix[(by+y)*img.Stride+bx*4:]
				for x := 0; x < 4; x++ {
					c := pal[bits&3]
					bits >>= 2
					row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c[0], c[1], c[2], 255
				}
			}
		}
	}
	return img
}

func rgb565(c uint16) [3]uint8 {
	r, g, b := c>>11&31, c>>5&63, c&31
	return [3]uint8{uint8(r<<3 | r>>2), uint8(g<<2 | g>>4), uint8(b<<3 | b>>2)}
}
