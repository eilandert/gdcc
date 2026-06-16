package dcc

import (
	"bytes"
	"testing"
)

func benchmarkMessage(size int) []byte {
	const header = "From: sender@example.net\r\nMessage-ID: <benchmark@example.net>\r\nContent-Type: text/plain\r\n\r\n"
	const chunk = "A representative mail body line with enough content for DCC fuzzy checksums.\r\n"
	msg := make([]byte, 0, size+len(header))
	msg = append(msg, header...)
	for len(msg) < size {
		msg = append(msg, chunk...)
	}
	return bytes.Clone(msg[:size])
}

func BenchmarkChecksums256K(b *testing.B) {
	msg := benchmarkMessage(256 << 10)
	b.SetBytes(int64(len(msg)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Checksums(msg)
	}
}
