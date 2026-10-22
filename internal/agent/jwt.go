package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var b64 = base64.RawURLEncoding

// Claims of the tokens the agent signs. Empty fields are left out.
type Claims struct {
	Sub string `json:"sub,omitempty"`
	Aud string `json:"aud,omitempty"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// SignHS256 signs claims valid from now for ttl.
func SignHS256(secret []byte, sub, aud string, ttl time.Duration) string {
	now := time.Now().Unix()
	payload, _ := json.Marshal(Claims{Sub: sub, Aud: aud, Iat: now, Exp: now + int64(ttl/time.Second)})
	signing := b64.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + b64.EncodeToString(mac.Sum(nil))
}

// SignPayload signs fields of its own with aud, iat and exp set: valid from now for ttl. A token with an audience is
// never a page session.
func SignPayload(secret []byte, aud string, fields map[string]any, ttl time.Duration) string {
	now := time.Now().Unix()
	claims := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		claims[k] = v
	}
	claims["aud"], claims["iat"], claims["exp"] = aud, now, now+int64(ttl/time.Second)
	payload, _ := json.Marshal(claims)
	signing := b64.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + b64.EncodeToString(mac.Sum(nil))
}

// VerifiedAudience is the payload of a token signed with secret, valid now and naming audience; false otherwise.
func VerifiedAudience(secret []byte, token, audience string, now time.Time) (map[string]any, bool) {
	if VerifyHS256(secret, token, audience, now) != nil {
		return nil, false
	}
	return VerifiedPayload(secret, token, now)
}

var errToken = errors.New("invalid token")

// VerifyHS256 accepts a compact HS256 token signed with secret, naming audience, not expired and already valid.
func VerifyHS256(secret []byte, token, audience string, now time.Time) error {
	payload, err := verifyHS256(secret, token, now)
	if err != nil {
		return err
	}
	switch aud := payload["aud"].(type) {
	case string:
		if aud == audience {
			return nil
		}
	case []any:
		for _, a := range aud {
			if a == audience {
				return nil
			}
		}
	}
	return errors.New("wrong audience")
}

// VerifySession accepts a page session token: signed with the pair's secret, valid now, and naming no audience — a
// token with one belongs to the agent's link, never to a page.
func VerifySession(secret []byte, token string, now time.Time) error {
	payload, err := verifyHS256(secret, token, now)
	if err != nil {
		return err
	}
	if _, ok := payload["aud"]; ok {
		return errors.New("not a session token")
	}
	return nil
}

func verifyHS256(secret []byte, token string, now time.Time) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errToken
	}
	rawHeader, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, errToken
	}
	var header map[string]any
	if json.Unmarshal(rawHeader, &header) != nil || header["alg"] != "HS256" {
		return nil, errToken
	}
	if _, ok := header["crit"]; ok {
		return nil, errToken
	}
	if b, ok := header["b64"]; ok && b != true {
		return nil, errToken
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, errToken
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errToken
	}
	rawPayload, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, errToken
	}
	var payload map[string]any
	if json.Unmarshal(rawPayload, &payload) != nil {
		return nil, errToken
	}
	nowSec := float64(now.Unix())
	for _, k := range []string{"iat", "exp", "nbf"} {
		if v, ok := payload[k]; ok {
			if _, isNum := v.(float64); !isNum {
				return nil, errToken
			}
		}
	}
	if exp, ok := payload["exp"].(float64); ok && exp <= nowSec {
		return nil, errors.New("token expired")
	}
	if nbf, ok := payload["nbf"].(float64); ok && nbf > nowSec {
		return nil, errors.New("token not yet valid")
	}
	return payload, nil
}

// VerifiedPayload is the payload of any token signed with the pair's secret and valid now, audience or not (what the
// previous runtime's verifySessionToken returned); false otherwise.
func VerifiedPayload(secret []byte, token string, now time.Time) (map[string]any, bool) {
	payload, err := verifyHS256(secret, token, now)
	return payload, err == nil
}

// SessionExpiry is the expiry (epoch seconds) of a valid page session token; false for any other token or one without
// an expiry.
func SessionExpiry(secret []byte, token string, now time.Time) (float64, bool) {
	payload, err := verifyHS256(secret, token, now)
	if err != nil {
		return 0, false
	}
	if _, ok := payload["aud"]; ok {
		return 0, false
	}
	exp, ok := payload["exp"].(float64)
	return exp, ok
}

// SessionSubject is the `sub` of a valid page session token (the machine agent signs its own with its name); false for
// any other token.
func SessionSubject(secret []byte, token string, now time.Time) (string, bool) {
	payload, err := verifyHS256(secret, token, now)
	if err != nil {
		return "", false
	}
	if _, ok := payload["aud"]; ok {
		return "", false
	}
	sub, ok := payload["sub"].(string)
	return sub, ok
}
