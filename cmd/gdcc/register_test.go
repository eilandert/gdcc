package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runReg(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(args, strings.NewReader(""), &out, &errb)
	return rc, out.String(), errb.String()
}

func TestCLIRegisterSavesAndPrintsEnv(t *testing.T) {
	ids := filepath.Join(t.TempDir(), "ids")
	rc, out, errb := runReg(t, []string{"--out", ids, "--client-id", "32", "--passwd", "secret", "register"})
	if rc != 0 {
		t.Fatalf("register rc=%d err=%q", rc, errb)
	}
	// Reports the saved file AND prints bare env-var lines.
	if !strings.Contains(out, "saved client-id 32 to "+ids) {
		t.Fatalf("missing saved-path line: %q", out)
	}
	if !strings.Contains(out, "\nGDCC_CLIENT_ID=32\n") || !strings.Contains(out, "\nGDCC_CLIENT_PASSWD=secret\n") {
		t.Fatalf("missing/!bare env lines: %q", out)
	}
	data, err := os.ReadFile(ids)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "32 secret") {
		t.Fatalf("ids file missing entry: %q", data)
	}
}

func TestCLIRegisterRequiresRealID(t *testing.T) {
	// Missing id (defaults to 0) and the anonymous id (1) are both rejected.
	for _, args := range [][]string{
		{"--out", filepath.Join(t.TempDir(), "ids"), "--passwd", "x", "register"},
		{"--out", filepath.Join(t.TempDir(), "ids"), "--client-id", "1", "--passwd", "x", "register"},
	} {
		rc, _, errb := runReg(t, args)
		if rc != 2 || !strings.Contains(errb, "client-id") {
			t.Fatalf("args %v: rc=%d err=%q", args, rc, errb)
		}
	}
}

func TestCLIRegisterRequiresPasswd(t *testing.T) {
	rc, _, errb := runReg(t, []string{"--out", filepath.Join(t.TempDir(), "ids"), "--client-id", "32", "register"})
	if rc != 2 || !strings.Contains(errb, "passwd") {
		t.Fatalf("rc=%d err=%q", rc, errb)
	}
}
