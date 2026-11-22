package mapslice

import (
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestPlan(t *testing.T) {
	for _, c := range []struct{ px, max, levels, tile int }{
		{15360, 512, 6, 480},
		{8192, 512, 5, 512},
		{500, 512, 1, 500},
		{1001, 256, 3, 251},
	} {
		if l, ts := Plan(c.px, c.max); l != c.levels || ts != c.tile {
			t.Errorf("Plan(%d, %d) = %d levels of %d px, want %d of %d", c.px, c.max, l, ts, c.levels, c.tile)
		}
	}
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "testmap")
	os.MkdirAll(filepath.Join(dir, "9"), 0o755)
	for _, f := range []string{"0_0.jpg", "15_15.jpg", "notes.txt"} { // an old 16 × 16 slicing, and a file of the user's
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	// West half red, east half blue.
	img := image.NewRGBA(image.Rect(0, 0, 1001, 1001))
	for y := range 1001 {
		for x := range 1001 {
			c := color.RGBA{200, 0, 0, 255}
			if x >= 500 {
				c = color.RGBA{0, 0, 200, 255}
			}
			img.Set(x, y, c)
		}
	}
	info, err := Write(dir, img, 256, 90, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info != (Info{Px: 1001, Tile: 251, Levels: 3, Ext: "jpg"}) {
		t.Errorf("info %+v", info)
	}
	var saved Info
	if b, err := os.ReadFile(filepath.Join(dir, InfoFile)); err != nil || json.Unmarshal(b, &saved) != nil || saved != info {
		t.Errorf("tiles.json: %+v %v", saved, err)
	}
	size := func(p string) (image.Image, image.Point) {
		f, err := os.Open(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		im, err := jpeg.Decode(f)
		if err != nil {
			t.Fatal(p, err)
		}
		return im, im.Bounds().Size()
	}
	for p, want := range map[string]image.Point{
		"2/0_0.jpg": {251, 251},
		"2/3_3.jpg": {248, 248}, // 1001 - 3 × 251
		"1/1_0.jpg": {250, 251}, // the level is 501 px
		"0/0_0.jpg": {251, 251},
	} {
		if _, got := size(p); got != want {
			t.Errorf("%s: %v, want %v", p, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "2", "4_0.jpg")); err == nil {
		t.Error("a tile past the picture's edge")
	}
	im, _ := size("0/0_0.jpg")
	if r, _, b, _ := im.At(20, 125).RGBA(); r>>8 < 180 || b>>8 > 30 {
		t.Errorf("level 0's west: %d, %d", r>>8, b>>8)
	}
	if r, _, b, _ := im.At(230, 125).RGBA(); b>>8 < 180 || r>>8 > 30 {
		t.Errorf("level 0's east: %d, %d", r>>8, b>>8)
	}
	for _, gone := range []string{"0_0.jpg", "15_15.jpg", "9"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s from before is still there", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Error("removed a file that isn't a tile")
	}
}
