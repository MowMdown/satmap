package pbo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Entry is one file in a PBO being read.
type Entry struct {
	Name     string // its path in the PBO, as stored (backslashes)
	Method   uint32 // 0 stored, cprsMethod LZSS-compressed
	OrigSize uint32 // its size once unpacked
	Size     uint32 // its size in the PBO
	Offset   int64  // where its bytes start in the file
}

// Archive is a PBO opened for reading: its prefix and its files.
type Archive struct {
	Prefix  string
	Entries []Entry
	f       *os.File
}

const cprsMethod = 0x43707273 // "Cprs"

// Open reads a PBO's header. Close it when done.
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	a, err := readHeader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	a.f = f
	return a, nil
}

// Close closes the PBO's file.
func (a *Archive) Close() error { return a.f.Close() }

func readHeader(f *os.File) (*Archive, error) {
	r := bufio.NewReader(f)
	var pos int64
	str := func() (string, error) {
		s, err := r.ReadString(0)
		pos += int64(len(s))
		if err != nil {
			return "", errors.New("the header ends early (not a PBO?)")
		}
		return s[:len(s)-1], nil
	}
	nums := func() ([5]uint32, error) {
		var v [5]uint32
		err := binary.Read(r, binary.LittleEndian, &v)
		pos += 20
		if err != nil {
			return v, errors.New("the header ends early (not a PBO?)")
		}
		return v, nil
	}
	a := &Archive{}
	var dataStart int64
	for {
		name, err := str()
		if err != nil {
			return nil, err
		}
		v, err := nums()
		if err != nil {
			return nil, err
		}
		if name == "" && v[0] == versMethod {
			// The properties: key, value, ... and an empty key.
			for {
				k, err := str()
				if err != nil {
					return nil, err
				}
				if k == "" {
					break
				}
				val, err := str()
				if err != nil {
					return nil, err
				}
				if strings.EqualFold(k, "prefix") {
					a.Prefix = val
				}
			}
			continue
		}
		if name == "" {
			dataStart = pos
			break
		}
		a.Entries = append(a.Entries, Entry{Name: name, Method: v[0], OrigSize: v[1], Size: v[4]})
	}
	off := dataStart
	for i := range a.Entries {
		a.Entries[i].Offset = off
		off += int64(a.Entries[i].Size)
	}
	if st, err := f.Stat(); err == nil && off > st.Size() {
		return nil, errors.New("the files run past the end (cut short?)")
	}
	return a, nil
}

// Read returns an entry's bytes, unpacked.
func (a *Archive) Read(e Entry) ([]byte, error) {
	b := make([]byte, e.Size)
	if _, err := a.f.ReadAt(b, e.Offset); err != nil && err != io.EOF {
		return nil, err
	}
	if e.Method == cprsMethod && e.OrigSize != e.Size {
		return unLZSS(b, int(e.OrigSize))
	}
	return b, nil
}

// unLZSS unpacks a PBO's compressed file: flag bytes, each bit a literal
// byte (1) or a 2-byte back reference (0): 12-bit distance, 4-bit length-3.
func unLZSS(in []byte, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	i := 0
	for len(out) < size {
		if i >= len(in) {
			return nil, errors.New("compressed data ends early")
		}
		flags := in[i]
		i++
		for bit := 0; bit < 8 && len(out) < size; bit++ {
			if flags&1 != 0 {
				if i >= len(in) {
					return nil, errors.New("compressed data ends early")
				}
				out = append(out, in[i])
				i++
			} else {
				if i+1 >= len(in) {
					return nil, errors.New("compressed data ends early")
				}
				dist := int(in[i]) | int(in[i+1]&0xF0)<<4
				n := int(in[i+1]&0x0F) + 3
				i += 2
				from := len(out) - dist
				for k := 0; k < n && len(out) < size; k++ {
					if from+k < 0 {
						out = append(out, ' ')
					} else {
						out = append(out, out[from+k])
					}
				}
			}
			flags >>= 1
		}
	}
	return out, nil
}
