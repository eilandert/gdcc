package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eilandert/gdcc/dcc"
)

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run([]string{"--version"}, strings.NewReader(""), &out, &errb); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "gdcc") {
		t.Errorf("version output %q", out.String())
	}
}

func TestUsageAndUnknown(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run(nil, strings.NewReader(""), &out, &errb); rc != 2 {
		t.Errorf("no-args rc=%d", rc)
	}
	out.Reset()
	errb.Reset()
	if rc := run([]string{"bogus"}, strings.NewReader(""), &out, &errb); rc != 2 {
		t.Errorf("unknown-op rc=%d", rc)
	}
}

func TestCksumOffline(t *testing.T) {
	msg := "From: a@b.c\r\nMessage-ID: <x@y>\r\n\r\n" +
		"This body is comfortably more than thirty characters long for validity.\r\n"
	var out, errb bytes.Buffer
	if rc := run([]string{"cksum"}, strings.NewReader(msg), &out, &errb); rc != 0 {
		t.Fatalf("rc=%d err=%s", rc, errb.String())
	}
	s := out.String()
	for _, want := range []string{"From:", "Message-ID:", "Body:"} {
		if !strings.Contains(s, want) {
			t.Errorf("cksum output missing %q:\n%s", want, s)
		}
	}
	// each line ends with the 4-word checksum form
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if fields := strings.Fields(line); len(fields) != 5 { // label + 4 hex words
			t.Errorf("malformed cksum line: %q", line)
		}
	}
}

func TestParseServers(t *testing.T) {
	if got := parseServers("", 0); got != nil {
		t.Errorf("empty spec → %v, want nil", got)
	}
	got := parseServers("a.example, b.example:9999 ,c.example", 0)
	want := []dcc.Server{
		{Host: "a.example", Port: 0},
		{Host: "b.example", Port: 9999},
		{Host: "c.example", Port: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d servers, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("server %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
