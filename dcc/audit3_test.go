package dcc

import (
	"context"
	"testing"
	"time"
)

// TestCancelDuringActiveReadIsPrompt: a context with no deadline, cancelled
// while a read is blocked on a silent server, must return well before the
// (long) Timeout — the watcher trips the read deadline.
func TestCancelDuringActiveReadIsPrompt(t *testing.T) {
	port, stop := fakeServer(t, true, 0) // silent: never replies
	defer stop()
	c := &Client{
		Servers: []Server{{Host: "127.0.0.1", Port: port}},
		Timeout: 10 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()

	start := time.Now()
	if _, err := c.CheckContext(ctx, []byte(testMsg)); err == nil {
		t.Fatal("cancelled context should fail")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel not prompt: %s (Timeout was 10s)", elapsed)
	}
}

func TestInvalidateAddr(t *testing.T) {
	c := &Client{}
	srv := Server{Host: "127.0.0.1", Port: 6277}
	if _, err := c.resolve(srv); err != nil {
		t.Fatal(err)
	}
	c.addrMu.Lock()
	_, ok := c.addrCache[srv]
	c.addrMu.Unlock()
	if !ok {
		t.Fatal("address should be cached after resolve")
	}
	c.invalidateAddr(srv)
	c.addrMu.Lock()
	_, ok = c.addrCache[srv]
	c.addrMu.Unlock()
	if ok {
		t.Error("address should be evicted after invalidateAddr")
	}
}
