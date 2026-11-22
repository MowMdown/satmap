package paa

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"
)

// The .lzo files were packed by the LZO library itself (python-lzo, levels 1
// and 9); the hashes are of the data before packing.
func TestUnLZO(t *testing.T) {
	for _, c := range []struct {
		file string
		size int
		sum  string
	}{
		{"mixed-1.lzo", 49705, "0d8de5a7c7667c22a6c7ea804678d886852ff27e2decdafa9220962c7b0d1016"},
		{"mixed-9.lzo", 49705, "0d8de5a7c7667c22a6c7ea804678d886852ff27e2decdafa9220962c7b0d1016"},
		{"far-9.lzo", 34000, "95cc450df1bc5377b6a56822a13ad84a6e7e63f8b1d3ac4d260615412d473296"},
	} {
		in, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		out, err := unLZO(in, c.size)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if sum := sha256.Sum256(out); hex.EncodeToString(sum[:]) != c.sum {
			t.Errorf("%s: unpacked to the wrong bytes", c.file)
		}
		// Cut short or with the wrong size, it fails instead of panicking.
		if _, err := unLZO(in[:len(in)/2], c.size); err == nil {
			t.Errorf("%s cut in half: no error", c.file)
		}
		if _, err := unLZO(in, c.size-1); err == nil {
			t.Errorf("%s one byte short: no error", c.file)
		}
	}
}

// paaFile wraps DXT1 data as a PAA with a tag and an empty palette.
func paaFile(w, h int, data []byte, packed bool) []byte {
	b := binary.LittleEndian.AppendUint16(nil, typeDXT1)
	b = append(b, "GGATCGVA"...)
	b = binary.LittleEndian.AppendUint32(b, 4)
	b = append(b, 1, 2, 3, 4)
	b = binary.LittleEndian.AppendUint16(b, 0)
	ww := w
	if packed {
		ww |= 0x8000
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(ww))
	b = binary.LittleEndian.AppendUint16(b, uint16(h))
	b = append(b, byte(len(data)), byte(len(data)>>8), byte(len(data)>>16))
	return append(b, data...)
}

func TestDecodeDXT1(t *testing.T) {
	// One 4×4 block: red and blue, the pixels picking 0, 1, 2, 3 in turn.
	red, blue := uint16(31<<11), uint16(31)
	blk := binary.LittleEndian.AppendUint16(nil, red)
	blk = binary.LittleEndian.AppendUint16(blk, blue)
	blk = binary.LittleEndian.AppendUint32(blk, 0xE4E4E4E4) // 0,1,2,3 on every row
	data := append(append([]byte{}, blk...), blk...)        // 8×4

	img, err := Decode(paaFile(8, 4, data, false))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 4 {
		t.Fatalf("size %v", img.Bounds())
	}
	want := [][3]uint8{{255, 0, 0}, {0, 0, 255}, {170, 0, 85}, {85, 0, 170}}
	for x := 0; x < 8; x++ {
		p := img.PixOffset(x, 3)
		got := [3]uint8{img.Pix[p], img.Pix[p+1], img.Pix[p+2]}
		if got != want[x%4] || img.Pix[p+3] != 255 {
			t.Errorf("pixel %d: %v, want %v", x, got, want[x%4])
		}
	}

	// A block with c0 <= c1 uses 3 colours and black.
	blk3 := binary.LittleEndian.AppendUint16(nil, blue)
	blk3 = binary.LittleEndian.AppendUint16(blk3, red)
	blk3 = binary.LittleEndian.AppendUint32(blk3, 0xFFFFFFFF)
	img, err = Decode(paaFile(4, 4, blk3, false))
	if err != nil {
		t.Fatal(err)
	}
	if p := img.PixOffset(2, 2); img.Pix[p] != 0 || img.Pix[p+1] != 0 || img.Pix[p+2] != 0 {
		t.Errorf("3-colour block's 4th colour isn't black: %v", img.Pix[p:p+3])
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := Decode([]byte{0x44, 0x44, 0, 0}); err == nil {
		t.Error("ARGB4444: no error")
	}
	if _, err := Decode(paaFile(8, 4, make([]byte, 8), false)); err == nil {
		t.Error("short mipmap: no error")
	}
	if _, err := Decode(paaFile(8, 4, []byte{1, 2, 3}, true)); err == nil {
		t.Error("bad LZO: no error")
	}
}
