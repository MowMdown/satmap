package pbo

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"testing"
	"time"
)

// read is a minimal PBO reader for the test: the prefix, and each file's
// path and bytes in order. It checks the SHA-1 at the end.
func read(t *testing.T, b []byte) (string, []File) {
	t.Helper()
	if len(b) < 21 || b[len(b)-21] != 0 {
		t.Fatal("no checksum")
	}
	body := b[:len(b)-21]
	if sum := sha1.Sum(body); !bytes.Equal(sum[:], b[len(b)-20:]) {
		t.Fatal("bad checksum")
	}
	p := 0
	cstr := func() string {
		i := bytes.IndexByte(body[p:], 0)
		s := string(body[p : p+i])
		p += i + 1
		return s
	}
	u32 := func() uint32 { v := binary.LittleEndian.Uint32(body[p:]); p += 4; return v }
	type ent struct {
		name string
		size uint32
	}
	var ents []ent
	prefix := ""
	for first := true; ; first = false {
		name := cstr()
		method, _, _, _, size := u32(), u32(), u32(), u32(), u32()
		if first {
			if name != "" || method != versMethod {
				t.Fatal("no Vers entry first")
			}
			for {
				k := cstr()
				if k == "" {
					break
				}
				v := cstr()
				if k == "prefix" {
					prefix = v
				}
			}
			continue
		}
		if name == "" {
			break
		}
		if method != 0 {
			t.Fatalf("%s is packed with method %x", name, method)
		}
		ents = append(ents, ent{name, size})
	}
	var files []File
	for _, e := range ents {
		files = append(files, File{e.name, body[p : p+int(e.size)]})
		p += int(e.size)
	}
	if p != len(body) {
		t.Fatalf("%d bytes left over", len(body)-p)
	}
	return prefix, files
}

func TestPackRoundTrip(t *testing.T) {
	in := []File{
		{"scripts/4_World/ReCON/b.c", []byte("class B {};")},
		{"scripts/3_Game/ReCON/a.c", []byte("class A {};")},
		{"config.cpp", []byte("class CfgPatches {};")},
		{"empty.txt", nil},
	}
	b, err := Pack("ReCON", in, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	prefix, out := read(t, b)
	if prefix != "ReCON" {
		t.Errorf("prefix %q", prefix)
	}
	want := []string{"config.cpp", `empty.txt`, `scripts\3_Game\ReCON\a.c`, `scripts\4_World\ReCON\b.c`}
	if len(out) != len(want) {
		t.Fatalf("%d files", len(out))
	}
	for i, w := range want {
		if out[i].Name != w {
			t.Errorf("file %d is %q, want %q", i, out[i].Name, w)
		}
	}
	if string(out[0].Data) != "class CfgPatches {};" || string(out[3].Data) != "class B {};" {
		t.Error("file contents")
	}
}

func TestPackRejects(t *testing.T) {
	if _, err := Pack("", []File{{"config.cpp", nil}}, time.Now()); err == nil {
		t.Error("an empty prefix")
	}
	if _, err := Pack("X", []File{{"a.c", nil}, {"A.C", nil}}, time.Now()); err == nil {
		t.Error("the same file twice")
	}
}
