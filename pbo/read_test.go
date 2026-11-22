package pbo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenRead(t *testing.T) {
	files := []File{
		{Name: "config.cpp", Data: []byte("class CfgPatches {};")},
		{Name: "layers/s_000_001_lco.paa", Data: bytes.Repeat([]byte{7}, 1000)},
		{Name: "layers/empty.txt", Data: nil},
	}
	b, err := Pack(`dz\worlds\x\data`, files, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "x.pbo")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Prefix != `dz\worlds\x\data` {
		t.Errorf("prefix %q", a.Prefix)
	}
	if len(a.Entries) != 3 {
		t.Fatalf("%d entries", len(a.Entries))
	}
	got := map[string][]byte{}
	for _, e := range a.Entries {
		d, err := a.Read(e)
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name] = d
	}
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "/", `\`)
		if !bytes.Equal(got[name], f.Data) {
			t.Errorf("%s: %d bytes, want %d", name, len(got[name]), len(f.Data))
		}
	}

	// Not a PBO, or cut short.
	bad := filepath.Join(t.TempDir(), "bad.pbo")
	os.WriteFile(bad, b[:40], 0o644)
	if _, err := Open(bad); err == nil {
		t.Error("cut-short PBO: no error")
	}
}

func TestUnLZSS(t *testing.T) {
	// "abcabcabcX": literals a b c, a back reference (distance 3, length 6),
	// a literal X. Flags: bits 0-2 literal, bit 3 reference, bit 4 literal.
	in := []byte{0x17, 'a', 'b', 'c', 3, 6 - 3, 'X'}
	out, err := unLZSS(in, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "abcabcabcX" {
		t.Errorf("got %q", out)
	}
	if _, err := unLZSS(in[:4], 10); err == nil {
		t.Error("cut short: no error")
	}
}
