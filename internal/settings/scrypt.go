package settings

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"strings"
)

// The previous runtime hashed a password with node's scrypt defaults: N=16384, r=8, p=1, a 16-byte salt and a 64-byte
// key, written "scrypt:<salt hex>:<key hex>".
const (
	scryptN      = 16384
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 64
	scryptSalt   = 16
	scryptPrefix = "scrypt:"
)

// IsHashedPassword says whether a stored password is a hash this runtime can check.
func IsHashedPassword(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, scryptPrefix)
}

// HashPassword is a stored password for plain with a random salt.
func HashPassword(plain string) string { return hashPassword(plain, cryptoRandom(scryptSalt)) }

func hashPassword(plain string, salt []byte) string {
	key := scrypt([]byte(plain), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	return scryptPrefix + hex.EncodeToString(salt) + ":" + hex.EncodeToString(key)
}

// VerifyPassword checks plain against a stored hash; anything that is not one never matches.
func VerifyPassword(plain, stored string) bool {
	if !strings.HasPrefix(stored, scryptPrefix) {
		return false
	}
	parts := strings.Split(stored, ":")
	if len(parts) < 3 {
		return false
	}
	salt := hexPrefix(parts[1])
	expected := hexPrefix(parts[2])
	if len(expected) == 0 {
		return false // node refuses a key length of 0
	}
	derived := scrypt([]byte(plain), salt, scryptN, scryptR, scryptP, len(expected))
	return subtle.ConstantTimeCompare(derived, expected) == 1
}

// hexPrefix decodes as Buffer.from(s, 'hex') does: pairs of hex digits up to the first that is not one.
func hexPrefix(s string) []byte {
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		b, err := hex.DecodeString(s[i : i+2])
		if err != nil {
			break
		}
		out = append(out, b[0])
	}
	return out
}

// scrypt is RFC 7914.
func scrypt(password, salt []byte, n, r, p, keyLen int) []byte {
	b, _ := pbkdf2.Key(sha256.New, string(password), salt, 1, p*128*r)
	xy := make([]uint32, 64*r)
	v := make([]uint32, 32*n*r)
	for i := 0; i < p; i++ {
		roMix(b[i*128*r:], r, n, v, xy)
	}
	key, _ := pbkdf2.Key(sha256.New, string(password), b, 1, keyLen)
	return key
}

func roMix(b []byte, r, n int, v, xy []uint32) {
	x, y := xy[:32*r], xy[32*r:]
	for i := range x {
		x[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	for i := 0; i < n; i++ {
		copy(v[i*32*r:], x)
		blockMix(x, y, r)
	}
	for i := 0; i < n; i++ {
		j := int(x[(2*r-1)*16] & uint32(n-1))
		for k := range x {
			x[k] ^= v[j*32*r+k]
		}
		blockMix(x, y, r)
	}
	for i, w := range x {
		binary.LittleEndian.PutUint32(b[i*4:], w)
	}
}

func blockMix(b, y []uint32, r int) {
	var x [16]uint32
	copy(x[:], b[(2*r-1)*16:])
	for i := 0; i < 2*r; i++ {
		for k := range x {
			x[k] ^= b[i*16+k]
		}
		salsa8(&x)
		copy(y[i*16:], x[:])
	}
	for i := 0; i < r; i++ {
		copy(b[i*16:], y[2*i*16:2*i*16+16])
		copy(b[(r+i)*16:], y[(2*i+1)*16:(2*i+1)*16+16])
	}
}

func salsa8(b *[16]uint32) {
	x := *b
	for i := 0; i < 8; i += 2 {
		x[4] ^= bits.RotateLeft32(x[0]+x[12], 7)
		x[8] ^= bits.RotateLeft32(x[4]+x[0], 9)
		x[12] ^= bits.RotateLeft32(x[8]+x[4], 13)
		x[0] ^= bits.RotateLeft32(x[12]+x[8], 18)
		x[9] ^= bits.RotateLeft32(x[5]+x[1], 7)
		x[13] ^= bits.RotateLeft32(x[9]+x[5], 9)
		x[1] ^= bits.RotateLeft32(x[13]+x[9], 13)
		x[5] ^= bits.RotateLeft32(x[1]+x[13], 18)
		x[14] ^= bits.RotateLeft32(x[10]+x[6], 7)
		x[2] ^= bits.RotateLeft32(x[14]+x[10], 9)
		x[6] ^= bits.RotateLeft32(x[2]+x[14], 13)
		x[10] ^= bits.RotateLeft32(x[6]+x[2], 18)
		x[3] ^= bits.RotateLeft32(x[15]+x[11], 7)
		x[7] ^= bits.RotateLeft32(x[3]+x[15], 9)
		x[11] ^= bits.RotateLeft32(x[7]+x[3], 13)
		x[15] ^= bits.RotateLeft32(x[11]+x[7], 18)
		x[1] ^= bits.RotateLeft32(x[0]+x[3], 7)
		x[2] ^= bits.RotateLeft32(x[1]+x[0], 9)
		x[3] ^= bits.RotateLeft32(x[2]+x[1], 13)
		x[0] ^= bits.RotateLeft32(x[3]+x[2], 18)
		x[6] ^= bits.RotateLeft32(x[5]+x[4], 7)
		x[7] ^= bits.RotateLeft32(x[6]+x[5], 9)
		x[4] ^= bits.RotateLeft32(x[7]+x[6], 13)
		x[5] ^= bits.RotateLeft32(x[4]+x[7], 18)
		x[11] ^= bits.RotateLeft32(x[10]+x[9], 7)
		x[8] ^= bits.RotateLeft32(x[11]+x[10], 9)
		x[9] ^= bits.RotateLeft32(x[8]+x[11], 13)
		x[10] ^= bits.RotateLeft32(x[9]+x[8], 18)
		x[12] ^= bits.RotateLeft32(x[15]+x[14], 7)
		x[13] ^= bits.RotateLeft32(x[12]+x[15], 9)
		x[14] ^= bits.RotateLeft32(x[13]+x[12], 13)
		x[15] ^= bits.RotateLeft32(x[14]+x[13], 18)
	}
	for i := range b {
		b[i] += x[i]
	}
}
