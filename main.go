// Command satmap makes ReCON's Live Map picture of a map from the map's own
// satellite tiles, on any OS, with no DayZ Tools needed.
//
// A DayZ map keeps its satellite picture as tiles, layers\S_<x>_<y>_lco.paa,
// in one of its data PBOs. Point satmap at that PBO, at the folder it is in
// (it looks through every PBO there) or at a folder of extracted .paa
// tiles, and it writes maps/<world>.jpg and its Live Map tiles,
// maps/<world>/, for ReCON's data folder (satmap -slice picture.jpg slices
// a picture you already have):
//
//	satmap "C:\Program Files (x86)\Steam\steamapps\common\DayZ"          (lists the maps it finds)
//	satmap -world enoch "C:\Program Files (x86)\Steam\steamapps\common\DayZ"
//	satmap "...\DayZ\!Workshop\@Namalsk Island"
//
// The tiles overlap their neighbours; satmap measures by how much and
// crops it, so it works on maps with different tile layouts (Chernarus,
// Livonia, Sakhal, Nasdara, Namalsk, Deer Isle, ...).
//
// The picture comes from game files: it is fine for your own ReCON, but
// don't share or publish it (see README.md).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/MowMdown/satmap/mapslice"
	"github.com/MowMdown/satmap/paa"
	"github.com/MowMdown/satmap/pbo"
)

// tileName matches a satellite tile's path (lower case, forward slashes).
var tileName = regexp.MustCompile(`(?:^|/)s_(\d{3})_(\d{3})_lco\.paa$`)

// tile is where one tile's bytes are: in a PBO, or a file.
type tile struct {
	arc   *pbo.Archive
	entry pbo.Entry
	file  string
}

func (t tile) read() ([]byte, error) {
	if t.arc != nil {
		return t.arc.Read(t.entry)
	}
	return os.ReadFile(t.file)
}

// layer is one map's tiles: where they are (the PBO, if any, and the
// folder in it) and the tiles by [x, y].
type layer struct {
	from  string
	dir   string
	tiles map[[2]int]tile
}

func (l *layer) where() string {
	if l.from == "" {
		return l.dir
	}
	return l.dir + " in " + l.from
}

func main() {
	log.SetFlags(0)
	world := flag.String("world", "", "the map's world name, for the file name (chernarusplus, enoch, sakhal, namalsk, ...); also picks the map when the PBOs hold more than one (default: from the tiles' path)")
	out := flag.String("o", "", "the picture to write (default maps/<world>.jpg)")
	px := flag.Int("px", 8192, "the picture's width and height in pixels (at most the tiles' own size)")
	size := flag.Int("size", 0, "the satellite picture's size in tile pixels, without the padding at its edges (default: the tiles' total, rounded down to a multiple of 256; on most maps 1 pixel is 1 m, so this is the map's size)")
	crop := flag.Int("crop", -1, "pixels to crop from each side of every tile (default: measured from the tiles)")
	quality := flag.Int("quality", 90, "JPEG quality, 1 to 100")
	tiles := flag.Bool("tiles", true, "also slice the picture into the Live Map's tiles, in maps/<world>/ next to it")
	tilePx := flag.Int("tile", 512, "the tiles' size in pixels, at most")
	slice := flag.String("slice", "", "slice this picture (a .jpg or .png you already have) into tiles, and nothing else: in the folder it is in when that has its name (maps/enoch/enoch.png), else in <its folder>/<its name>/")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: satmap [flags] PBO-or-folder...\n       satmap -slice picture.jpg\n\nMakes maps/<world>.jpg and its tiles, maps/<world>/, for ReCON's Live Map from a map's satellite tiles (layers\\S_*_lco.paa); with -slice, slices a picture you already have into tiles.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *slice != "" {
		if err := slicePicture(*slice, *tilePx, *quality); err != nil {
			log.Fatal(err)
		}
		return
	}
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	layers, closeAll, err := findTiles(flag.Args())
	if err != nil {
		log.Fatal(err)
	}
	defer closeAll()
	l, name, err := pickLayer(layers, *world)
	if err != nil {
		log.Fatal(err)
	}
	nx, ny := 0, 0
	for k := range l.tiles {
		nx, ny = max(nx, k[0]+1), max(ny, k[1]+1)
	}
	log.Printf("%s: %d tiles (%d × %d) in %s", name, len(l.tiles), nx, ny, l.where())

	first, err := decode(l, [2]int{nx / 2, ny / 2})
	if err != nil {
		log.Fatal(err)
	}
	ts := first.Bounds().Dx()
	if first.Bounds().Dy() != ts {
		log.Fatalf("the tiles aren't square (%v)", first.Bounds().Size())
	}
	c := *crop
	if c < 0 {
		if c, err = measureCrop(l, nx, ny, first); err != nil {
			log.Fatal(err)
		}
	}
	stride := ts - 2*c
	if stride <= 0 {
		log.Fatalf("cropping %d px from each side of %d px tiles leaves nothing", c, ts)
	}
	total := min(nx, ny) * stride
	src := *size
	if src <= 0 {
		src = total / 256 * 256
	}
	if src > total {
		log.Fatalf("-size %d is bigger than the tiles' %d px", src, total)
	}
	dst := min(*px, src)
	log.Printf("tiles %d px, %d cropped from each side: %d px in all, %d used, written at %d px", ts, c, total, src, dst)

	img, err := stitch(l, nx, ny, c, stride, src, dst)
	if err != nil {
		log.Fatal(err)
	}
	path := *out
	if path == "" {
		path = filepath.Join("maps", name+".jpg")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: max(1, min(100, *quality))}); err != nil {
		f.Close()
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: put it in ReCON's data folder as maps/%s.jpg", path, name)
	if *tiles {
		dir := filepath.Join(filepath.Dir(path), name)
		if _, err := mapslice.Write(dir, img, *tilePx, *quality, log.Printf); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote the tiles in %s: put the folder in ReCON's data folder as maps/%s", dir, name)
	}
}

// slicePicture slices the picture at path into tiles in sliceDir(path).
func slicePicture(path string, maxTile, quality int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	dir := sliceDir(path)
	log.Printf("%s: %d × %d px", path, img.Bounds().Dx(), img.Bounds().Dy())
	if _, err := mapslice.Write(dir, img, maxTile, quality, log.Printf); err != nil {
		return err
	}
	log.Printf("wrote the tiles in %s", dir)
	return nil
}

// sliceDir is the folder the tiles of the picture at path go in: the
// folder it is in when that is named after it (maps/enoch/enoch.png),
// else the folder next to it named after it (maps/enoch.png: maps/enoch).
func sliceDir(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if dir := filepath.Dir(path); strings.EqualFold(filepath.Base(dir), name) {
		return dir
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}

// findTiles finds satellite tiles in the PBOs and .paa files at paths
// (folders are searched), grouped by the folder they are in.
func findTiles(paths []string) (map[string]*layer, func(), error) {
	layers := map[string]*layer{}
	var open []*pbo.Archive
	closeAll := func() {
		for _, a := range open {
			a.Close()
		}
	}
	add := func(from, full string, t tile) {
		full = strings.ToLower(strings.ReplaceAll(full, `\`, "/"))
		m := tileName.FindStringSubmatch(full)
		if m == nil {
			return
		}
		dir := full[:len(full)-len(m[0])]
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		key := from + "|" + dir // two PBOs with the same map stay apart
		if layers[key] == nil {
			layers[key] = &layer{from: from, dir: dir, tiles: map[[2]int]tile{}}
		}
		layers[key].tiles[[2]int{x, y}] = t
	}
	addPBO := func(p string) error {
		a, err := pbo.Open(p)
		if err != nil {
			return err
		}
		n := 0
		for _, e := range a.Entries {
			full := e.Name
			if a.Prefix != "" {
				full = a.Prefix + `\` + e.Name
			}
			if tileName.MatchString(strings.ToLower(strings.ReplaceAll(full, `\`, "/"))) {
				add(p, full, tile{arc: a, entry: e})
				n++
			}
		}
		if n == 0 {
			a.Close()
		} else {
			open = append(open, a)
		}
		return nil
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		if !st.IsDir() {
			if strings.EqualFold(filepath.Ext(p), ".pbo") {
				err = addPBO(p)
			} else {
				add("", p, tile{file: p})
			}
			if err != nil {
				closeAll()
				return nil, nil, err
			}
			continue
		}
		err = filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch strings.ToLower(filepath.Ext(q)) {
			case ".pbo":
				if err := addPBO(q); err != nil {
					log.Printf("skipping %s: %v", q, err)
				}
			case ".paa":
				add("", q, tile{file: q})
			}
			return nil
		})
		if err != nil {
			closeAll()
			return nil, nil, err
		}
	}
	if len(layers) == 0 {
		closeAll()
		return nil, nil, fmt.Errorf("no satellite tiles (layers\\S_*_lco.paa) in %s", strings.Join(paths, ", "))
	}
	return layers, closeAll, nil
}

// pickLayer picks the map's tiles (the one whose path has world in it when
// there are several) and its world name.
func pickLayer(layers map[string]*layer, world string) (*layer, string, error) {
	var names []string
	var hits []*layer
	for _, l := range layers {
		names = append(names, worldOf(l.dir)+": "+l.where())
		if world == "" || strings.Contains(strings.ToLower(l.from+"|"+l.dir), strings.ToLower(world)) {
			hits = append(hits, l)
		}
	}
	sort.Strings(names)
	if len(hits) != 1 {
		why := "more than one set of map tiles is here"
		if len(hits) == 0 {
			why = "no tiles here are " + world + "'s"
		}
		return nil, "", fmt.Errorf("%s; pick one with -world:\n  %s", why, strings.Join(names, "\n  "))
	}
	name := strings.ToLower(world)
	if name == "" || strings.ContainsAny(name, `./\:`) { // -world picked by path
		name = worldOf(hits[0].dir)
	}
	if name == "" {
		return nil, "", fmt.Errorf("can't tell the world name from %s; give it with -world", hits[0].dir)
	}
	return hits[0], name, nil
}

// worldOf guesses the world name from the tiles' folder:
// dz/worlds/chernarusplus/data/layers/ is chernarusplus.
func worldOf(dir string) string {
	parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '/' || r == ':' })
	for i, p := range parts {
		if p == "worlds" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	for i, p := range parts {
		if p == "data" && i > 0 {
			return parts[i-1]
		}
	}
	return ""
}

func decode(l *layer, at [2]int) (*image.RGBA, error) {
	t, ok := l.tiles[at]
	if !ok {
		return nil, fmt.Errorf("tile %03d_%03d is missing", at[0], at[1])
	}
	b, err := t.read()
	if err != nil {
		return nil, fmt.Errorf("tile %03d_%03d: %w", at[0], at[1], err)
	}
	img, err := paa.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("tile %03d_%03d: %w", at[0], at[1], err)
	}
	return img, nil
}

// measureCrop finds how much neighbouring tiles overlap (they repeat each
// other's edges exactly) and returns half of it: what to crop from each
// side so the tiles meet edge to edge.
func measureCrop(l *layer, nx, ny int, a *image.RGBA) (int, error) {
	x, y := nx/2, ny/2
	if x+1 >= nx || y+1 >= ny {
		return 0, fmt.Errorf("too few tiles to measure their overlap; give -crop")
	}
	right, err := decode(l, [2]int{x + 1, y})
	if err != nil {
		return 0, err
	}
	below, err := decode(l, [2]int{x, y + 1})
	if err != nil {
		return 0, err
	}
	ts := a.Bounds().Dx()
	best, bestDiff := 0, 1e18
	for o := 2; o <= ts/2; o += 2 {
		d := (edgeDiff(a, right, o, false) + edgeDiff(a, below, o, true)) / 2
		if d < bestDiff {
			best, bestDiff = o, d
		}
	}
	if bestDiff > 4 {
		return 0, fmt.Errorf("couldn't measure the tiles' overlap (best: %d px, off by %.1f); give -crop", best, bestDiff)
	}
	return best / 2, nil
}

// edgeDiff is how much a's last o columns (rows, if down) differ from b's
// first o, on average per channel.
func edgeDiff(a, b *image.RGBA, o int, down bool) float64 {
	ts := a.Bounds().Dx()
	var sum, n float64
	for i := 0; i < ts; i++ {
		for k := 0; k < o; k++ {
			var pa, pb int
			if down {
				pa, pb = a.PixOffset(i, ts-o+k), b.PixOffset(i, k)
			} else {
				pa, pb = a.PixOffset(ts-o+k, i), b.PixOffset(k, i)
			}
			for ch := 0; ch < 3; ch++ {
				d := float64(a.Pix[pa+ch]) - float64(b.Pix[pb+ch])
				if d < 0 {
					d = -d
				}
				sum += d
			}
			n += 3
		}
	}
	return sum / n
}

// stitch lays the tiles out (cropped by c, stride apart), keeps the top-left
// src×src pixels and scales them to dst×dst, averaging. It works a row of
// tiles at a time.
func stitch(l *layer, nx, ny, c, stride, src, dst int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, dst, dst))
	acc := map[int][]uint32{} // output row -> r, g, b, count per pixel
	row := func(oy int) []uint32 {
		r := acc[oy]
		if r == nil {
			r = make([]uint32, dst*4)
			acc[oy] = r
		}
		return r
	}
	flush := func(upTo int) { // finish output rows < upTo
		for oy, r := range acc {
			if oy >= upTo {
				continue
			}
			pix := img.Pix[oy*img.Stride:]
			for x := 0; x < dst; x++ {
				if n := r[x*4+3]; n > 0 {
					pix[x*4] = uint8(r[x*4] / n)
					pix[x*4+1] = uint8(r[x*4+1] / n)
					pix[x*4+2] = uint8(r[x*4+2] / n)
				}
				pix[x*4+3] = 255
			}
			delete(acc, oy)
		}
	}
	for ty := 0; ty < ny && ty*stride < src; ty++ {
		// Decode this row of tiles in parallel.
		tiles := make([]*image.RGBA, nx)
		errs := make([]error, nx)
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU())
		for tx := 0; tx < nx && tx*stride < src; tx++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(tx int) {
				defer wg.Done()
				defer func() { <-sem }()
				tiles[tx], errs[tx] = decode(l, [2]int{tx, ty})
			}(tx)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		for tx, t := range tiles {
			if t == nil {
				continue
			}
			for y := 0; y < stride; y++ {
				sy := ty*stride + y
				if sy >= src {
					break
				}
				r := row(int(int64(sy) * int64(dst) / int64(src)))
				for x := 0; x < stride; x++ {
					sx := tx*stride + x
					if sx >= src {
						break
					}
					p := t.PixOffset(c+x, c+y)
					o := int(int64(sx)*int64(dst)/int64(src)) * 4
					r[o] += uint32(t.Pix[p])
					r[o+1] += uint32(t.Pix[p+1])
					r[o+2] += uint32(t.Pix[p+2])
					r[o+3]++
				}
			}
		}
		end := min(src, (ty+1)*stride)
		flush(int(int64(end) * int64(dst) / int64(src)))
	}
	flush(dst)
	return img, nil
}
