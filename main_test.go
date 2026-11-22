package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// colourAt is the test map: every 4×4 block one colour, from where it is.
func colourAt(bx, by int) [3]uint8 {
	return [3]uint8{uint8(bx * 8), uint8(by * 8), uint8((bx*3 + by*5) % 32 * 8)}
}

// writeTiles cuts the test map into n×n tiles of ts px that overlap by 2c,
// as one-colour DXT1 blocks (so they decode exactly), in a layers folder.
func writeTiles(t *testing.T, n, ts, c int) string {
	dir := filepath.Join(t.TempDir(), "dz", "worlds", "testmap", "data", "layers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stride := ts - 2*c
	for ty := 0; ty < n; ty++ {
		for tx := 0; tx < n; tx++ {
			var data []byte
			for by := 0; by < ts/4; by++ {
				for bx := 0; bx < ts/4; bx++ {
					col := colourAt((tx*stride)/4+bx, (ty*stride)/4+by)
					c565 := uint16(col[0]>>3)<<11 | uint16(col[1]>>2)<<5 | uint16(col[2]>>3)
					data = binary.LittleEndian.AppendUint16(data, c565)
					data = binary.LittleEndian.AppendUint16(data, c565)
					data = binary.LittleEndian.AppendUint32(data, 0)
				}
			}
			b := binary.LittleEndian.AppendUint16(nil, 0xFF01)
			b = binary.LittleEndian.AppendUint16(b, 0) // no palette
			b = binary.LittleEndian.AppendUint16(b, uint16(ts))
			b = binary.LittleEndian.AppendUint16(b, uint16(ts))
			b = append(b, byte(len(data)), byte(len(data)>>8), byte(len(data)>>16))
			b = append(b, data...)
			name := filepath.Join(dir, fmt.Sprintf("S_%03d_%03d_lco.paa", tx, ty))
			if err := os.WriteFile(name, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func TestStitch(t *testing.T) {
	const n, ts, c = 5, 64, 8
	dir := writeTiles(t, n, ts, c)
	layers, closeAll, err := findTiles([]string{filepath.Dir(dir)})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	l, name, err := pickLayer(layers, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "testmap" || len(l.tiles) != n*n {
		t.Fatalf("world %q, %d tiles", name, len(l.tiles))
	}
	mid, err := decode(l, [2]int{2, 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := measureCrop(l, n, n, mid)
	if err != nil || got != c {
		t.Fatalf("measured crop %d (%v), want %d", got, err, c)
	}
	stride := ts - 2*c
	src := n*stride - 16
	img, err := stitch(l, n, n, c, stride, src, src)
	if err != nil {
		t.Fatal(err)
	}
	// Every pixel is the map's own colour: the overlap is gone and the
	// padding (c px) is cropped.
	for y := 0; y < src; y++ {
		for x := 0; x < src; x++ {
			want := colourAt((x+c)/4, (y+c)/4)
			p := img.PixOffset(x, y)
			for ch := 0; ch < 3; ch++ {
				if d := int(img.Pix[p+ch]) - int(want[ch]); d < -8 || d > 8 {
					t.Fatalf("pixel %d,%d: %v, want %v", x, y, img.Pix[p:p+3], want)
				}
			}
		}
	}
	// Scaled down, every pixel is still filled.
	small, err := stitch(l, n, n, c, stride, src, 50)
	if err != nil {
		t.Fatal(err)
	}
	for i := 3; i < len(small.Pix); i += 4 {
		if small.Pix[i] != 255 {
			t.Fatal("a pixel of the scaled picture wasn't filled")
		}
	}
}

func TestWorldOf(t *testing.T) {
	for dir, want := range map[string]string{
		"dz/worlds/chernarusplus/data/layers/": "chernarusplus",
		"p:/dz/worlds/enoch/data/layers/":      "enoch",
		"nst/namalsk/data/layers/":             "namalsk",
		"layers/":                              "",
	} {
		if got := worldOf(dir); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
}

func TestSliceDir(t *testing.T) {
	for in, want := range map[string]string{
		"maps/enoch.png":              "maps/enoch",
		"maps/enoch/enoch.png":        "maps/enoch",
		"maps/Enoch/enoch.jpg":        "maps/Enoch",
		"maps/other/enoch.png":        "maps/other/enoch",
		"enoch.png":                   "enoch",
		"maps/enoch/enoch_winter.png": "maps/enoch/enoch_winter",
	} {
		in, want = filepath.FromSlash(in), filepath.FromSlash(want)
		if got := sliceDir(in); got != want {
			t.Errorf("sliceDir(%q) = %q, want %q", in, got, want)
		}
	}
}
