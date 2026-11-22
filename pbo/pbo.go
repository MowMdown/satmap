// Package pbo writes PBO archives, the files DayZ (and Arma) load mods from.
//
// A PBO is a header, the files' bytes one after another, and a SHA-1 of it
// all:
//
//	header entries, each: name (NUL-terminated), then five little-endian
//	uint32s: packing method, original size, reserved, timestamp, data size
//	  - first, a "Vers" entry (empty name, method 0x56657273, zeros),
//	    followed by NUL-terminated key/value strings ("prefix", "ReCON")
//	    and an empty string
//	  - one entry per file: its path with backslashes, method 0 (stored),
//	    sizes, timestamp
//	  - an empty entry (empty name, all zeros) ending the header
//	then each file's bytes in the same order
//	then a 0 byte and the SHA-1 of everything before it
package pbo

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// File is one file to pack: its path inside the PBO ("scripts/3_Game/x.c")
// and its bytes.
type File struct {
	Name string
	Data []byte
}

const versMethod = 0x56657273 // "Vers"

// Pack builds a PBO with the prefix (the path its files have in the game)
// and files, stored uncompressed, sorted by path with config.cpp first.
func Pack(prefix string, files []File, modified time.Time) ([]byte, error) {
	if prefix == "" || strings.ContainsAny(prefix, "\x00") {
		return nil, errors.New("pbo: the prefix is empty or has a NUL")
	}
	list := append([]File(nil), files...)
	for i := range list {
		list[i].Name = strings.ReplaceAll(strings.TrimLeft(list[i].Name, `/\`), "/", `\`)
		if list[i].Name == "" || strings.Contains(list[i].Name, "\x00") {
			return nil, fmt.Errorf("pbo: bad file name %q", list[i].Name)
		}
	}
	sort.SliceStable(list, func(a, b int) bool {
		ca, cb := strings.EqualFold(list[a].Name, "config.cpp"), strings.EqualFold(list[b].Name, "config.cpp")
		if ca != cb {
			return ca
		}
		return strings.ToLower(list[a].Name) < strings.ToLower(list[b].Name)
	})
	seen := map[string]bool{}
	for _, f := range list {
		if seen[strings.ToLower(f.Name)] {
			return nil, fmt.Errorf("pbo: %s is in the list twice", f.Name)
		}
		seen[strings.ToLower(f.Name)] = true
	}

	var buf bytes.Buffer
	entry := func(name string, method, size, stamp, dataSize uint32) {
		buf.WriteString(name)
		buf.WriteByte(0)
		for _, v := range []uint32{method, size, 0, stamp, dataSize} {
			binary.Write(&buf, binary.LittleEndian, v)
		}
	}
	entry("", versMethod, 0, 0, 0)
	for _, s := range []string{"prefix", prefix, ""} {
		buf.WriteString(s)
		buf.WriteByte(0)
	}
	stamp := uint32(modified.Unix())
	for _, f := range list {
		if len(f.Data) > 1<<31 {
			return nil, fmt.Errorf("pbo: %s is too big", f.Name)
		}
		entry(f.Name, 0, uint32(len(f.Data)), stamp, uint32(len(f.Data)))
	}
	entry("", 0, 0, 0, 0)
	for _, f := range list {
		buf.Write(f.Data)
	}
	sum := sha1.Sum(buf.Bytes())
	buf.WriteByte(0)
	buf.Write(sum[:])
	return buf.Bytes(), nil
}
