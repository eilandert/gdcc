package dcc

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadExpected reads testdata/expected.tsv (name\tlabel\thex) produced by the
// real dccproc (testdata/gen_expected.sh) into name -> label -> hex.
func loadExpected(t *testing.T) map[string]map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "expected.tsv"))
	if err != nil {
		t.Fatalf("open expected.tsv: %v", err)
	}
	defer f.Close()

	exp := map[string]map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("bad expected line: %q", line)
		}
		name, label, hex := parts[0], parts[1], parts[2]
		if exp[name] == nil {
			exp[name] = map[string]string{}
		}
		exp[name][label] = hex
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan expected.tsv: %v", err)
	}
	return exp
}

// TestChecksumParity verifies every checksum gdcc emits matches the real dccproc
// byte-for-byte. Labels dccproc emits that gdcc does not yet implement (Fuz1/
// Fuz2 until ported) are reported, not failed, unless DCC_PARITY_STRICT=1.
func TestChecksumParity(t *testing.T) {
	exp := loadExpected(t)
	strict := os.Getenv("DCC_PARITY_STRICT") == "1"

	files, _ := filepath.Glob(filepath.Join("testdata", "corpus", "*.eml"))
	if len(files) == 0 {
		t.Fatal("no corpus files")
	}

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			want := exp[name]
			if want == nil {
				t.Fatalf("no expected checksums for %s (run gen_expected.sh)", name)
			}
			msg, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			got := map[string]string{}
			for _, ck := range Checksums(msg) {
				got[ck.Label] = ck.Sum.String()
			}

			// Every checksum we produce must match.
			for label, hex := range got {
				w, ok := want[label]
				if !ok {
					t.Errorf("%s: gdcc emitted %s but dccproc did not", name, label)
					continue
				}
				if hex != w {
					t.Errorf("%s %s:\n  gdcc:    %s\n  dccproc: %s", name, label, hex, w)
				}
			}
			// Report (or fail, when strict) checksums dccproc has that we lack.
			for label := range want {
				if _, ok := got[label]; !ok {
					if strict {
						t.Errorf("%s: missing checksum %s (not yet ported)", name, label)
					} else {
						t.Logf("%s: %s not yet ported", name, label)
					}
				}
			}
		})
	}
}
