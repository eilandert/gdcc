package dcc

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeServer answers DCC queries. If silent, it never replies (a black hole).
func fakeServer(t *testing.T, silent bool, cur uint32) (port int, stop func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, ra, err := conn.ReadFromUDP(buf)
			select {
			case <-done:
				return
			default:
			}
			if err != nil || n < 28 || silent {
				continue
			}
			ncks := (n - 24 - 4 - 16) / 18
			ans := make([]byte, 24+ncks*8+16)
			binary.BigEndian.PutUint16(ans[0:2], uint16(len(ans)))
			ans[2] = dccPktVers
			ans[3] = opAnswer
			copy(ans[8:20], buf[8:20]) // echo h,p,r
			for i := 0; i < ncks; i++ {
				binary.BigEndian.PutUint32(ans[24+i*8:], cur)
			}
			conn.WriteToUDP(ans, ra)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, func() { close(done); conn.Close() }
}

const testMsg = "From: a@b.c\r\nMessage-ID: <x@y>\r\n\r\n" +
	"The quick brown fox jumps over the lazy dog while the cat watches today here.\r\n"

// TestConcurrentCheck shares one Client across many goroutines (as serve mode
// does). Run under -race it proves ensureIDs is race-free.
func TestConcurrentCheck(t *testing.T) {
	port, stop := fakeServer(t, false, 0)
	defer stop()
	c := &Client{Servers: []Server{{Host: "127.0.0.1", Port: port}}, Timeout: 2 * time.Second}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Check([]byte(testMsg)); err != nil {
				t.Errorf("check: %v", err)
			}
		}()
	}
	wg.Wait()
	if c.hid == 0 {
		t.Error("hid never initialised")
	}
}

// TestParallelQueryFastWins races a dead server against a fast one; the answer
// must arrive well before the full timeout.
func TestParallelQueryFastWins(t *testing.T) {
	dead, stopD := fakeServer(t, true, 0)
	defer stopD()
	fast, stopF := fakeServer(t, false, 7)
	defer stopF()

	c := &Client{
		Servers: []Server{{Host: "127.0.0.1", Port: dead}, {Host: "127.0.0.1", Port: fast}},
		Timeout: 4 * time.Second,
	}
	start := time.Now()
	res, err := c.Check([]byte(testMsg))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("parallel query took %v, expected fast win", d)
	}
	if len(res.Counts) == 0 {
		t.Error("no counts from fast server")
	}
}

func TestReportUsesOneTotalDeadline(t *testing.T) {
	dead1, stop1 := fakeServer(t, true, 0)
	defer stop1()
	dead2, stop2 := fakeServer(t, true, 0)
	defer stop2()

	c := &Client{
		Servers: []Server{
			{Host: "127.0.0.1", Port: dead1},
			{Host: "127.0.0.1", Port: dead2},
		},
		Timeout: 400 * time.Millisecond,
	}
	start := time.Now()
	if err := c.Report([]byte(testMsg)); err == nil {
		t.Fatal("silent servers should fail")
	}
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Fatalf("report took %s; timeout was applied per server", elapsed)
	}
}

func TestCheckContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{
		Servers: []Server{{Host: "127.0.0.1", Port: 1}},
		Timeout: 5 * time.Second,
	}
	start := time.Now()
	if _, err := c.CheckContext(ctx, []byte(testMsg)); err == nil {
		t.Fatal("canceled context should fail")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("canceled check took %s", elapsed)
	}
}

func TestResolveCachesAddress(t *testing.T) {
	c := &Client{}
	srv := Server{Host: "127.0.0.1", Port: 6277}
	first, err := c.resolve(srv)
	if err != nil {
		t.Fatal(err)
	}
	firstExpiry := c.addrCache[srv].exp
	second, err := c.resolve(srv)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("live cache entry was resolved again")
	}
	if got := c.addrCache[srv].exp; !got.Equal(firstExpiry) {
		t.Error("cache hit unexpectedly refreshed the DNS TTL")
	}
}

func BenchmarkResolveCached(b *testing.B) {
	c := &Client{}
	srv := Server{Host: "127.0.0.1", Port: 6277}
	if _, err := c.resolve(srv); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.resolve(srv); err != nil {
			b.Fatal(err)
		}
	}
}
