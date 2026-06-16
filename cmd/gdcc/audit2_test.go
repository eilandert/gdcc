package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafetyCheck(t *testing.T) {
	if err := (serveConfig{listen: ":8080"}).safetyCheck(); err == nil {
		t.Error(":8080 without token should be refused")
	}
	if err := (serveConfig{listen: "127.0.0.1:8080"}).safetyCheck(); err != nil {
		t.Errorf("loopback without token should be allowed: %v", err)
	}
	if err := (serveConfig{listen: ":8080", token: "x"}).safetyCheck(); err != nil {
		t.Errorf("token set should allow any bind: %v", err)
	}
	if err := (serveConfig{unix: "/tmp/s"}).safetyCheck(); err != nil {
		t.Errorf("unix-only should be allowed: %v", err)
	}
}

func TestIsLoopbackListen(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true,
		":8080": false, "0.0.0.0:8080": false, "10.0.0.5:8080": false,
	} {
		if got := isLoopbackListen(addr); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestReadCapped(t *testing.T) {
	if b, err := readCapped(strings.NewReader("12345"), 5); err != nil || len(b) != 5 {
		t.Errorf("exact max: %d bytes err %v", len(b), err)
	}
	if _, err := readCapped(strings.NewReader("123456"), 5); err != errTooLarge {
		t.Errorf("over max: want errTooLarge, got %v", err)
	}
}

// TestReportIsInstrumented confirms /report latency is now recorded (it was
// previously excluded from the histogram).
func TestReportIsInstrumented(t *testing.T) {
	port, stop := fakeDCC(t, 0, 0)
	defer stop()
	c := testClient(port)
	m := newMetrics()
	h := m.instrument("report", reportHandler(c, m))
	h(httptest.NewRecorder(), httptest.NewRequest("POST", "/report", strings.NewReader(probe)))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "gdcc_latency_seconds_count 1") {
		t.Errorf("report not counted in latency histogram:\n%s", rec.Body.String())
	}
}

func TestServeConcurrencyGate(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	sem := make(chan struct{}, 1)
	gate := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next(w, r)
			default:
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
	}
	h := gate(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release })
	go h(httptest.NewRecorder(), httptest.NewRequest("POST", "/check", nil))
	<-entered
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/check", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("second concurrent request = %d, want 503", rec.Code)
	}
	close(release)
}
