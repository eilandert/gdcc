package dcc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIdentityFile(t *testing.T) {
	const ids = `# comment line
# id 100 is a server, skipped (below client min? no, but anon test)
1 anonpass
32768,delay=100 sekrit1 sekrit2
40000 second
`
	id, ok := ParseIdentityFile(strings.NewReader(ids))
	if !ok {
		t.Fatal("expected an identity")
	}
	// id 1 is anon (skipped); first usable is 32768 with options stripped
	if id.ClientID != 32768 || id.Password != "sekrit1" {
		t.Errorf("got id=%d pw=%q, want 32768/sekrit1", id.ClientID, id.Password)
	}
}

func TestParseIdentityFileNone(t *testing.T) {
	if _, ok := ParseIdentityFile(strings.NewReader("# only comments\n1 anon\n")); ok {
		t.Error("expected no usable (non-anon) identity")
	}
}

func TestResolveIdentityFlags(t *testing.T) {
	id := ResolveIdentity(12345, "pw")
	if id.ClientID != 12345 || id.Password != "pw" {
		t.Errorf("flags should win: %+v", id)
	}
	// anon flags fall through to anonymous when no env/file
	t.Setenv("GDCC_CLIENT_ID", "")
	t.Setenv("GDCC_CLIENT_PASSWD", "")
	t.Setenv("DCC_IDS", filepath.Join(t.TempDir(), "nonexistent"))
	if got := ResolveIdentity(0, ""); !got.Anonymous() {
		t.Errorf("want anonymous, got %+v", got)
	}
}

func TestResolveIdentityFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ids")
	if err := os.WriteFile(p, []byte("50000 filepass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GDCC_CLIENT_ID", "")
	t.Setenv("GDCC_CLIENT_PASSWD", "")
	t.Setenv("DCC_IDS", p)
	id := ResolveIdentity(0, "")
	if id.ClientID != 50000 || id.Password != "filepass" {
		t.Errorf("file identity not loaded: %+v", id)
	}
}
