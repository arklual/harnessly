package settings

import (
	"encoding/hex"
	"testing"
)

func TestScryptVectors(t *testing.T) {
	// RFC 7914, section 12, and the hash node's crypto.scrypt wrote for "pass" with this salt.
	if got := hex.EncodeToString(scrypt([]byte("password"), []byte("NaCl"), 1024, 8, 16, 64)); got != "fdbabe1c9d3472007856e7190d01e9fe7c6ad7cbc8237830e77376634b3731622eaf30d92e22a3886ff109279d9830dac727afb94a83ee6d8360cbdfa2cc0640" {
		t.Fatal(got)
	}
	if got := hex.EncodeToString(scrypt([]byte(""), []byte(""), 16, 1, 1, 64)); got != "77d6576238657b203b19ca42c18a0497f16b4844e3074ae8dfdffa3fede21442fcd0069ded0948f8326a753a0fc81f17e8d3e0fb2e0d3628cf35e20c38d18906" {
		t.Fatal(got)
	}
	stored := hashPassword("pass", make([]byte, 16))
	if stored != nodeHash {
		t.Fatal(stored)
	}
	if !VerifyPassword("pass", stored) || VerifyPassword("pas", stored) || VerifyPassword("pass", "plain") {
		t.Fatal("verify")
	}
}

const nodeHash = "scrypt:00000000000000000000000000000000:598997e98f4659227a7e76e12e6dd2c8399087265ee249f631c14be51e36b206b3bdc4af99b18331e8c62cbea2cb11bba02f69161c8f47bdbef4a6f39f372cec"
