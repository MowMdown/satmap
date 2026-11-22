// Package mapslice cuts a map's picture into the tiles the Live Map loads:
// a pyramid of levels, so the page draws only a few tiles whatever the
// zoom, instead of every full-size tile when zoomed out.
//
// In dir: <level>/<column>_<row>.jpg (column 0 at the west edge, row 0 at
// the north) and tiles.json describing them. Level 0 is the whole picture
// in one tile; each level after it has twice the columns and rows, at
// twice the detail; the last has the picture's own pixels. Every tile is
// tile × tile px, but those on the east and south edges can be narrower.
package mapslice

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
)

// InfoFile is the file in dir that describes the tiles.
const InfoFile = "tiles.json"

// Info is what InfoFile holds.
type Info struct {
	Px     int    `json:"px"`     // the picture's width and height
	Tile   int    `json:"tile"`   // a tile's width and height
	Levels int    `json:"levels"` // levels 0 to Levels-1
	Ext    string `json:"ext"`    // the tiles' file extension
}

// Plan is how many levels a px-wide picture takes with tiles of at most
// maxTile px, and the tiles' size.
func Plan(px, maxTile int) (levels, tile int) {
	n := 0
	for ceilDiv(px, 1<<n) > maxTile {
		n++
	}
	return n + 1, ceilDiv(px, 1<<n)
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// old matches what an earlier slicing left in dir: level folders and tiles
// sliced into dir itself.
var oldDir = regexp.MustCompile(`^\d{1,2}$`)
var oldTile = regexp.MustCompile(`(?i)^\d{1,3}_\d{1,3}\.(jpg|jpeg|png|webp)$`)

// Write slices img (square) into dir, replacing tiles already there, as
// JPEGs of the given quality with tiles of at most maxTile px. logf, if
// not nil, is told of each level.
func Write(dir string, img image.Image, maxTile, quality int, logf func(string, ...any)) (Info, error) {
	b := img.Bounds()
	if b.Dx() != b.Dy() || b.Dx() == 0 {
		return Info{}, fmt.Errorf("the picture isn't square (%d × %d)", b.Dx(), b.Dy())
	}
	if maxTile < 16 {
		return Info{}, fmt.Errorf("tiles of %d px are too small", maxTile)
	}
	levels, tile := Plan(b.Dx(), maxTile)
	info := Info{Px: b.Dx(), Tile: tile, Levels: levels, Ext: "jpg"}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return info, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return info, err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Name() == InfoFile:
			err = os.Remove(p)
		case e.IsDir() && oldDir.MatchString(e.Name()):
			err = os.RemoveAll(p)
		case !e.IsDir() && oldTile.MatchString(e.Name()):
			err = os.Remove(p)
		}
		if err != nil {
			return info, err
		}
	}

	cur, ok := img.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		cur = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(cur, cur.Bounds(), img, b.Min, draw.Src)
	}
	img = nil
	for k := levels - 1; k >= 0; k-- {
		n := ceilDiv(cur.Bounds().Dx(), tile)
		logf("level %d: %d × %d tiles, %d px across", k, n, n, cur.Bounds().Dx())
		if err := writeLevel(filepath.Join(dir, strconv.Itoa(k)), cur, tile, n, quality); err != nil {
			return info, err
		}
		if k > 0 {
			cur = half(cur)
		}
	}

	js, _ := json.Marshal(info)
	return info, os.WriteFile(filepath.Join(dir, InfoFile), append(js, '\n'), 0o644)
}

// writeLevel writes the n × n tiles of one level.
func writeLevel(dir string, img *image.RGBA, tile, n, quality int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	jobs := make(chan image.Point)
	errs := make(chan error, n*n)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for at := range jobs {
				r := image.Rect(at.X*tile, at.Y*tile, (at.X+1)*tile, (at.Y+1)*tile).Intersect(img.Bounds())
				errs <- writeJPEG(filepath.Join(dir, fmt.Sprintf("%d_%d.jpg", at.X, at.Y)), img.SubImage(r), quality)
			}
		}()
	}
	for y := range n {
		for x := range n {
			jobs <- image.Pt(x, y)
		}
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func writeJPEG(path string, img image.Image, quality int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: max(1, min(100, quality))}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// half is img at half its size (rounded up), each pixel the average of the
// 2 × 2 it replaces (fewer at an odd edge).
func half(img *image.RGBA) *image.RGBA {
	sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
	w, h := ceilDiv(sw, 2), ceilDiv(sh, 2)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	var wg sync.WaitGroup
	band := ceilDiv(h, runtime.NumCPU())
	for y0 := 0; y0 < h; y0 += band {
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			for y := y0; y < y1; y++ {
				sy0, sy1 := 2*y, min(2*y+1, sh-1)
				r0, r1 := img.Pix[sy0*img.Stride:], img.Pix[sy1*img.Stride:]
				o := out.Pix[y*out.Stride:]
				for x := range w {
					a, c := 8*x, min(8*x+4, 4*(sw-1))
					for i := range 4 {
						o[4*x+i] = uint8((uint32(r0[a+i]) + uint32(r0[c+i]) + uint32(r1[a+i]) + uint32(r1[c+i]) + 2) / 4)
					}
				}
			}
		}(y0, min(h, y0+band))
	}
	wg.Wait()
	return out
}
