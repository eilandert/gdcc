package main

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eilandert/gdcc/dcc"
)

// fakeDCC is a minimal DCC UDP server for tests: it echoes the query's
// transaction id and answers every queried checksum with the given count.
func fakeDCC(t *testing.T, cur, prev uint32) (port int, stop func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, raddr, err := conn.ReadFromUDP(buf)
			select {
			case <-done:
				return
			default:
			}
			if err != nil {
				continue
			}
			if n < 28 {
				continue
			}
			// number of checksums in the query
			ncks := (n - 24 - 4 - 16) / 18
			h := binary.BigEndian.Uint32(buf[8:12])
			p := binary.BigEndian.Uint32(buf[12:16])
			r := binary.BigEndian.Uint32(buf[16:20])

			ansLen := 24 + ncks*8 + 16
			ans := make([]byte, ansLen)
			binary.BigEndian.PutUint16(ans[0:2], uint16(ansLen))
			ans[2] = 12 // pkt_vers
			ans[3] = 4  // DCC_OP_ANSWER
			binary.BigEndian.PutUint32(ans[8:12], h)
			binary.BigEndian.PutUint32(ans[12:16], p)
			binary.BigEndian.PutUint32(ans[16:20], r)
			off := 24
			for i := 0; i < ncks; i++ {
				binary.BigEndian.PutUint32(ans[off:off+4], cur)
				binary.BigEndian.PutUint32(ans[off+4:off+8], prev)
				off += 8
			}
			conn.WriteToUDP(ans, raddr)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, func() { close(done); conn.Close() }
}

func testClient(port int) *dcc.Client {
	return &dcc.Client{
		Servers: []dcc.Server{{Host: "127.0.0.1", Port: port}},
		Timeout: 2 * time.Second,
	}
}

const probe = "From: a@b.c\r\nMessage-ID: <x@y>\r\n\r\n" +
	"The quick brown fox jumps over the lazy dog while the cat watches today here.\r\n"

func TestServeHealthz(t *testing.T) {
	m := newMetrics()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) }).ServeHTTP(rec, req)
	_ = m
	if rec.Body.String() != "ok\n" {
		t.Errorf("healthz body %q", rec.Body.String())
	}
}

func TestServeMetrics(t *testing.T) {
	m := newMetrics()
	m.inc(&m.checkTotal)
	m.verdictInc("reject")
	m.observe(0.02)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"gdcc_check_total 1",
		`gdcc_verdict_total{verdict="reject"} 1`,
		"gdcc_latency_seconds_count 1",
		`gdcc_latency_seconds_bucket{le="+Inf"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	}
}

func TestServeAuth(t *testing.T) {
	cfg := serveConfig{token: "sekret"}
	h := cfg.auth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/check", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: code %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/check", nil)
	req.Header.Set("X-GDCC-Token", "sekret")
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("with token: code %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/check", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("bearer: code %d", rec.Code)
	}
}

func TestServeCheckE2E(t *testing.T) {
	port, stop := fakeDCC(t, dccManyForTest, 0)
	defer stop()
	m := newMetrics()
	h := checkHandler(testClient(port), m)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/check", strings.NewReader(probe)))
	if rec.Code != 200 {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var resp checkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Action != "reject" || resp.Bulk == nil {
		t.Errorf("many body should reject with bulk: %+v", resp)
	}
	if len(resp.Counts) == 0 {
		t.Errorf("expected per-checksum counts")
	}
}

func TestServeReportE2E(t *testing.T) {
	port, stop := fakeDCC(t, 0, 0)
	defer stop()
	m := newMetrics()
	h := reportHandler(testClient(port), m)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/report", strings.NewReader(probe)))
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var resp map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp["reported"] {
		t.Errorf("report should succeed: %v", resp)
	}
}

func TestServeMethodNotAllowed(t *testing.T) {
	m := newMetrics()
	h := checkHandler(testClient(1), m)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/check", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /check code %d", rec.Code)
	}
}

// dccManyForTest is the DCC "many" sentinel (DCC_TGTS_TOO_MANY).
const dccManyForTest = 0x00fffff0
