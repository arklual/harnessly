package link

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"
)

// A request of the replicas' hop is signed with the secret they share: an HMAC over the method, the path with its
// query, the time it was made and the body. The secret itself never travels, a request that was changed on the way or
// kept for later is refused, and the comparison takes the same time whatever the answer.
const (
	signHeader = "X-Replica-Signature"
	timeHeader = "X-Replica-Time"
	// signSkew is how far the caller's clock may be from ours.
	signSkew = 30 * time.Second
)

func (c *Cluster) signature(method, target, at string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(c.Secret))
	for _, part := range []string{method, target, at} {
		mac.Write([]byte(part))
		mac.Write([]byte{'\n'})
	}
	mac.Write(body)
	return mac.Sum(nil)
}

// sign puts the signature of a request to another replica in its headers.
func (c *Cluster) sign(h http.Header, method, target string, body []byte) {
	at := strconv.FormatInt(c.now().Unix(), 10)
	h.Set(timeHeader, at)
	h.Set(signHeader, hex.EncodeToString(c.signature(method, target, at, body)))
}

// verified reports whether the request was signed by a replica now, and leaves its body readable again.
func (c *Cluster) verified(r *http.Request) bool {
	if c.Secret == "" {
		return false
	}
	at := r.Header.Get(timeHeader)
	sec, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return false
	}
	if d := c.now().Sub(time.Unix(sec, 0)); d > signSkew || d < -signSkew {
		return false
	}
	got, err := hex.DecodeString(r.Header.Get(signHeader))
	if err != nil {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxPayload+1))
	if err != nil || len(body) > MaxPayload {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return hmac.Equal(got, c.signature(r.Method, r.URL.RequestURI(), at, body))
}
