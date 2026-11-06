package secrets

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BrowserValue resolves a TOTP URI to its current RFC 6238 code. Other values are unchanged.
// Errors never include the URI or its key.
func BrowserValue(value string, now time.Time) (string, error) {
	if !strings.HasPrefix(value, "otpauth://") {
		return value, nil
	}
	invalid := errors.New("Invalid TOTP configuration in the safe")
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" {
		return "", invalid
	}
	q := u.Query()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(strings.ToUpper(q.Get("secret")), "="))
	if err != nil || len(key) == 0 {
		return "", invalid
	}
	algorithm := sha1.New
	switch strings.ToUpper(q.Get("algorithm")) {
	case "", "SHA1":
	case "SHA256":
		algorithm = sha256.New
	case "SHA512":
		algorithm = sha512.New
	default:
		return "", invalid
	}
	digits, period := 6, 30
	if q.Has("digits") {
		digits, err = strconv.Atoi(q.Get("digits"))
		if err != nil {
			return "", invalid
		}
	}
	if q.Has("period") {
		period, err = strconv.Atoi(q.Get("period"))
		if err != nil {
			return "", invalid
		}
	}
	if (digits != 6 && digits != 8) || period < 1 || period > 86400 || now.Unix() < 0 {
		return "", invalid
	}
	return totp(key, algorithm, uint64(now.Unix()/int64(period)), digits), nil
}

func totp(key []byte, algorithm func() hash.Hash, counter uint64, digits int) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], counter)
	h := hmac.New(algorithm, key)
	_, _ = h.Write(b[:])
	sum := h.Sum(nil)
	offset := int(sum[len(sum)-1] & 15)
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1000000)
	if digits == 8 {
		modulus = 100000000
	}
	return fmt.Sprintf("%0*d", digits, code%modulus)
}
