package dcc

import (
	"crypto/md5"
	"testing"
)

func TestVerifyAnswerSig(t *testing.T) {
	passwd := passwd16("s3cret")
	buf := append([]byte("a-dcc-answer-payload-of-some-length"), make([]byte, sigLen)...)
	h := md5.New()
	h.Write(passwd)
	h.Write(buf[:len(buf)-sigLen])
	copy(buf[len(buf)-sigLen:], h.Sum(nil))

	if !verifyAnswerSig(buf, passwd) {
		t.Error("correctly signed answer should verify")
	}
	tampered := append([]byte(nil), buf...)
	tampered[0] ^= 0xff
	if verifyAnswerSig(tampered, passwd) {
		t.Error("tampered answer must fail verification")
	}
	wrongPw := passwd16("other")
	if verifyAnswerSig(buf, wrongPw) {
		t.Error("wrong password must fail verification")
	}
	// anonymous (no password) has nothing to verify and always passes
	if !verifyAnswerSig([]byte("anything-at-all"), nil) {
		t.Error("anonymous answer should pass")
	}
	// too short to hold a signature
	if verifyAnswerSig([]byte("x"), passwd) {
		t.Error("short buffer must fail")
	}
}
