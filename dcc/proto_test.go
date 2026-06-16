package dcc

import (
	"crypto/md5"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func sampleChecksums() []Checksum {
	return Checksums([]byte("From: a@b.c\r\nMessage-ID: <x@y>\r\n\r\n" +
		"This body is comfortably more than thirty characters long for validity.\r\n"))
}

func TestBuildQueryAnon(t *testing.T) {
	cks := sampleChecksums()
	n := reportableCount(cks)
	if n == 0 {
		t.Fatal("no reportable checksums")
	}
	nums := opNums{h: 0x11223344, p: 0x55667788, r: 9, t: 1}
	pkt := buildQuery(opQuery, dccIDAnon, nums, 0, cks, nil)

	if got := int(binary.BigEndian.Uint16(pkt[0:2])); got != len(pkt) {
		t.Errorf("len field %d != packet %d", got, len(pkt))
	}
	if len(pkt) != hdrLen+4+int(n)*ckLen+sigLen {
		t.Errorf("packet length %d unexpected", len(pkt))
	}
	if pkt[2] != dccPktVers {
		t.Errorf("pkt_vers %d", pkt[2])
	}
	if pkt[3] != opQuery {
		t.Errorf("op %d", pkt[3])
	}
	if binary.BigEndian.Uint32(pkt[4:8]) != dccIDAnon {
		t.Errorf("sender not anon")
	}
	if binary.BigEndian.Uint32(pkt[24:28]) != 0 {
		t.Errorf("query tgts must be 0")
	}
	// anonymous signature is all zero
	for _, b := range pkt[len(pkt)-sigLen:] {
		if b != 0 {
			t.Fatalf("anon signature not zero")
		}
	}
}

func TestBuildQuerySigned(t *testing.T) {
	cks := sampleChecksums()
	pw := passwd16("s3cret")
	nums := opNums{h: 1, p: 2, r: 3, t: 1}
	pkt := buildQuery(opReport, 1000, nums, 1, cks, pw)

	h := md5.New()
	h.Write(pw)
	h.Write(pkt[:len(pkt)-sigLen])
	want := h.Sum(nil)
	if got := pkt[len(pkt)-sigLen:]; string(got) != string(want) {
		t.Errorf("signature mismatch\n got %x\nwant %x", got, want)
	}
	if binary.BigEndian.Uint32(pkt[24:28]) != 1 {
		t.Errorf("report tgts not set")
	}
}

// synthAnswer builds a DCC_OP_ANSWER echoing nums with the given per-ck counts.
func synthAnswer(nums opNums, counts []answerCount) []byte {
	pktLen := hdrLen + len(counts)*8 + sigLen
	buf := make([]byte, pktLen)
	binary.BigEndian.PutUint16(buf[0:2], uint16(pktLen))
	buf[2] = dccPktVers
	buf[3] = opAnswer
	binary.BigEndian.PutUint32(buf[8:12], nums.h)
	binary.BigEndian.PutUint32(buf[12:16], nums.p)
	binary.BigEndian.PutUint32(buf[16:20], nums.r)
	off := hdrLen
	for _, c := range counts {
		binary.BigEndian.PutUint32(buf[off:off+4], c.cur)
		binary.BigEndian.PutUint32(buf[off+4:off+8], c.prev)
		off += 8
	}
	return buf
}

func TestParseAnswerRoundTrip(t *testing.T) {
	nums := opNums{h: 7, p: 8, r: 9}
	in := []answerCount{{3, 2}, {dccTgtsTooMany, dccTgtsTooMany}, {5, 4}}
	buf := synthAnswer(nums, in)

	got, err := parseAnswer(buf, uint32(len(in)), nums)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("count %d: got %+v want %+v", i, got[i], in[i])
		}
	}

	// wrong transaction id → mismatch
	if _, err := parseAnswer(buf, uint32(len(in)), opNums{h: 1, p: 2, r: 3}); err != errMismatch {
		t.Errorf("expected mismatch, got %v", err)
	}
}

func TestVerdict(t *testing.T) {
	// body checksum at "many" → reject
	r := Result{Counts: []CkCount{
		{Type: CkFrom, Cur: 1},
		{Type: CkBody, Cur: dccTgtsTooMany},
	}}
	v := r.Verdict()
	if v.Action != ActionReject || v.Bulk == nil || *v.Bulk != dccTgtsTooMany {
		t.Errorf("expected reject with bulk, got %v", v.Action)
	}

	// server whitelist → accept
	w := Result{Counts: []CkCount{{Type: CkBody, Cur: dccTgtsOK}}}
	if w.Verdict().Action != ActionAccept {
		t.Errorf("expected accept")
	}

	// low counts → unknown; but a numeric threshold can reject
	lo := Result{Counts: []CkCount{{Type: CkFuz1, Cur: 50}}}
	if lo.Verdict().Action != ActionUnknown {
		t.Errorf("expected unknown at default threshold")
	}
	if lo.VerdictThreshold(10).Action != ActionReject {
		t.Errorf("expected reject at threshold 10")
	}
}

// TestLiveQuery hits a real DCC server. Skipped unless DCC_LIVE=1 (needs UDP
// 6277 egress). Set DCC_SERVERS to override the default public pool.
func TestLiveQuery(t *testing.T) {
	if os.Getenv("DCC_LIVE") != "1" {
		t.Skip("set DCC_LIVE=1 to run a live anonymous DCC query")
	}
	host := DefaultServer
	if s := os.Getenv("DCC_SERVERS"); s != "" {
		host = s
	}
	c := &Client{
		Servers: []Server{{Host: host}},
		Timeout: 8 * time.Second,
		Verbose: true,
		Log:     func(s string) { t.Log(s) },
	}
	res, err := c.Check([]byte("From: test@example.com\r\nMessage-ID: <live@example.com>\r\n\r\n" +
		"This is a unique-ish gdcc live probe body well over thirty characters.\r\n"))
	if err != nil {
		t.Fatalf("live query: %v", err)
	}
	for _, ck := range res.Counts {
		t.Logf("%-10s cur=%d prev=%d", ck.Label, ck.Cur, ck.Prev)
	}
	v := res.Verdict()
	t.Logf("verdict: %s", v.Action)
}
