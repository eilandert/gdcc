package dcc

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzChecksums feeds arbitrary bytes through the full checksum path. It must
// never panic and must be deterministic — the checksum machinery (MIME-ish
// body scan, URL/HTML state machines, UTF-8/punycode) is the part most exposed
// to hostile input.
func FuzzChecksums(f *testing.F) {
	// seed from the parity corpus
	files, _ := filepath.Glob(filepath.Join("testdata", "corpus", "*.eml"))
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte(""))
	f.Add([]byte("From: a@b\r\n\r\nbody"))
	f.Add([]byte("Content-Type: text/html\r\n\r\n<a href=http://xn--e1a.example/>&#x68;ttp"))

	f.Fuzz(func(t *testing.T, msg []byte) {
		got := Checksums(msg)
		// determinism: same input → same output
		again := Checksums(msg)
		if len(got) != len(again) {
			t.Fatalf("non-deterministic checksum count: %d vs %d", len(got), len(again))
		}
		for i := range got {
			if got[i] != again[i] {
				t.Fatalf("non-deterministic checksum %d: %+v vs %+v", i, got[i], again[i])
			}
		}
		// every emitted checksum renders to the fixed 4-word form
		for _, ck := range got {
			if len(ck.Sum.String()) != 35 { // 4*8 hex + 3 spaces
				t.Fatalf("bad checksum render: %q", ck.Sum.String())
			}
		}
	})
}
