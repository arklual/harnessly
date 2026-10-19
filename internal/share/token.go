package share

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"

	"harnessly/internal/jscompat"
)

// The share-read token: the platform's share route signs one per view and this runtime checks it. Its key is derived
// from the pair's secret with HKDF under its own info string, so a share token is never a session token and a session
// token never a share token.

const tokenMaxAge = 120

// TokenConfig names the token: the HKDF salt and the audience the share route signs with.
type TokenConfig struct {
	Salt, Audience string
}

func (c TokenConfig) key(secret []byte) []byte {
	k, err := hkdf.Key(sha256.New, secret, []byte(c.Salt), "share-read/v1", 32)
	if err != nil {
		return nil
	}
	return k
}

var shareBaseRe = regexp.MustCompile(`^https://[a-z0-9.-]+(?::\d{1,5})?/share/[A-Za-z0-9_-]{12}/$`)

// ParseShareBase is the link prefix the platform sent ("https://<share host>/share/<route key>/"); "" when sharing is
// not set up (no header, or a malformed one).
func ParseShareBase(v string) string {
	if shareBaseRe.MatchString(v) {
		return v
	}
	return ""
}

func b64(s string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	return b, err == nil
}

// VerifyToken checks a share-read token for one link: the viewer it names, or 401 (no, forged, expired or overlong
// token) or 403 (a valid token for another link). It checks what jose's jwtVerify checked for the previous runtime:
// HS256, the audience, iat/exp/sid/viewer present, issued at most two minutes ago and not in the future.
func (c TokenConfig) VerifyToken(token, id string, secret []byte, now time.Time) (string, int) {
	if token == "" || len(secret) == 0 {
		return "", 401
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", 401
	}
	rawHeader, ok := b64(parts[0])
	if !ok {
		return "", 401
	}
	var header map[string]any
	if json.Unmarshal(rawHeader, &header) != nil || header["alg"] != "HS256" {
		return "", 401
	}
	if _, crit := header["crit"]; crit {
		return "", 401
	}
	sig, ok := b64(parts[2])
	if !ok {
		return "", 401
	}
	mac := hmac.New(sha256.New, c.key(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return "", 401
	}
	rawPayload, ok := b64(parts[1])
	if !ok {
		return "", 401
	}
	var payload map[string]any
	if json.Unmarshal(rawPayload, &payload) != nil || payload == nil {
		return "", 401
	}
	for _, k := range []string{"iat", "exp", "sid", "viewer"} {
		if _, ok := payload[k]; !ok {
			return "", 401
		}
	}
	epoch := math.Floor(float64(now.UnixMilli()) / 1000)
	if nbf, ok := payload["nbf"]; ok {
		f, isNum := nbf.(float64)
		if !isNum || f > epoch {
			return "", 401
		}
	}
	exp, isNum := payload["exp"].(float64)
	if !isNum || exp <= epoch {
		return "", 401
	}
	iat, isNum := payload["iat"].(float64)
	if !isNum || epoch-iat > tokenMaxAge || epoch-iat < 0 {
		return "", 401
	}
	switch aud := payload["aud"].(type) {
	case string:
		if aud != c.Audience {
			return "", 401
		}
	case []any:
		found := false
		for _, a := range aud {
			found = found || a == c.Audience
		}
		if !found {
			return "", 401
		}
	default:
		return "", 401
	}
	if exp-iat > tokenMaxAge {
		return "", 401
	}
	viewer, isStr := payload["viewer"].(string)
	if !isStr || viewer == "" || jscompat.Len(viewer) > 256 {
		return "", 401
	}
	if sid, isStr := payload["sid"].(string); !isStr || sid != id {
		return "", 403
	}
	return viewer, 0
}
