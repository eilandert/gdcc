package dcc

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteIdentityFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids")
	if err := WriteIdentityFile(path, Identity{ClientID: 32, Password: "pw1"}); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("ids perm = %o, want 600", perm)
		}
	}

	f, _ := os.Open(path)
	id, ok := ParseIdentityFile(f)
	_ = f.Close()
	if !ok || id.ClientID != 32 || id.Password != "pw1" {
		t.Fatalf("round-trip = %+v ok=%v", id, ok)
	}

	// Re-register the same id with a new password: it must overwrite, not
	// duplicate, leaving exactly one line for id 32.
	if err := WriteIdentityFile(path, Identity{ClientID: 32, Password: "pw2"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "\n32 ") != 0 || strings.Count(string(data), "32 ") != 1 {
		t.Fatalf("expected one entry for id 32: %q", data)
	}
	f, _ = os.Open(path)
	id, ok = ParseIdentityFile(f)
	_ = f.Close()
	if !ok || id.Password != "pw2" {
		t.Fatalf("re-register did not overwrite: %+v", id)
	}
}

func TestWriteIdentityFilePreservesOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids")
	const seed = "# comment\n100 otherpass\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteIdentityFile(path, Identity{ClientID: 32, Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"# comment", "100 otherpass", "32 pw"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %q", want, data)
		}
	}
}

func TestWriteIdentityFileRejectsBad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids")
	if err := WriteIdentityFile(path, Identity{ClientID: 1, Password: "x"}); err == nil {
		t.Fatal("expected error for anonymous id")
	}
	if err := WriteIdentityFile(path, Identity{ClientID: 32, Password: ""}); err == nil {
		t.Fatal("expected error for empty password")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("rejected register must not create the file")
	}
}
