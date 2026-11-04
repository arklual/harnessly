package secrets

import (
	"encoding/base32"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B uses the decimal sequence repeated/truncated to these key lengths.
func totpVectorKey(n int) string {
	key := make([]byte, n)
	for i := range key {
		key[i] = byte('0' + (i+1)%10)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
}

func TestBrowserValueRFC6238(t *testing.T) {
	vectors := []struct {
		at                   int64
		sha1, sha256, sha512 string
	}{
		{59, "94287082", "46119246", "90693936"},
		{1111111109, "07081804", "68084774", "25091201"},
		{1111111111, "14050471", "67062674", "99943326"},
		{1234567890, "89005924", "91819424", "93441116"},
		{2000000000, "69279037", "90698825", "38618901"},
		{20000000000, "65353130", "77737706", "47863826"},
	}
	for _, v := range vectors {
		for _, alg := range []struct {
			name string
			size int
			want string
		}{{"SHA1", 20, v.sha1}, {"SHA256", 32, v.sha256}, {"SHA512", 64, v.sha512}} {
			t.Run(fmt.Sprintf("%s/%d", alg.name, v.at), func(t *testing.T) {
				q := url.Values{"secret": {totpVectorKey(alg.size)}, "algorithm": {alg.name}, "digits": {"8"}, "period": {"30"}}
				uri := "otpauth://totp/Example:account?" + q.Encode()
				got, err := BrowserValue(uri, time.Unix(v.at, 0))
				if err != nil || got != alg.want {
					t.Errorf("RFC 6238 code = %q, want %q; error=%v", got, alg.want, err)
				}
			})
		}
	}
}

func TestBrowserValueDefaultsAndPeriod(t *testing.T) {
	key := totpVectorKey(20)
	uri := "otpauth://totp/Example?secret=" + key
	for _, tc := range []struct {
		uri  string
		at   int64
		want string
	}{
		{uri, 59, "287082"},
		{uri, 1111111109, "081804"},
		{uri + "&algorithm=sha1&digits=8", 59, "94287082"},
		{uri + "&period=60&digits=8", 119, "94287082"},
		{"otpauth://totp/Example?secret=" + strings.ToLower(key) + "&digits=8", 59, "94287082"},
	} {
		got, err := BrowserValue(tc.uri, time.Unix(tc.at, 0))
		if err != nil || got != tc.want {
			t.Errorf("defaults/period code = %q, want %q; error=%v", got, tc.want, err)
		}
	}
	for _, value := range []string{"", fakeValue, "plain\nmultiline\nvalue"} {
		got, err := BrowserValue(value, time.Unix(59, 0))
		if err != nil || got != value {
			t.Error("non-TOTP value changed")
		}
	}
}

func TestBrowserValueErrorsDoNotExposeURI(t *testing.T) {
	key := totpVectorKey(20)
	base := "otpauth://totp/private-account?secret=" + key
	cases := []string{
		"otpauth://hotp/private-account?secret=" + key,
		"otpauth://totp/private-account?secret=",
		"otpauth://totp/private-account?secret=" + url.QueryEscape("invalid!key"),
		"otpauth://totp/%zz?secret=" + key,
		base + "&algorithm=MD5", base + "&digits=7", base + "&digits=0", base + "&digits=word",
		base + "&period=0", base + "&period=-1", base + "&period=86401", base + "&period=word",
	}
	var publicError string
	for i, uri := range append(cases, base) {
		now := time.Unix(59, 0)
		if i == len(cases) {
			now = time.Unix(-1, 0)
		}
		got, err := BrowserValue(uri, now)
		if err == nil || got != "" {
			t.Errorf("case %d: invalid TOTP returned a value or no error", i)
			continue
		}
		if publicError == "" {
			publicError = err.Error()
		}
		if err.Error() != publicError || strings.Contains(err.Error(), uri) || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "private-account") || strings.Contains(err.Error(), "otpauth://") {
			t.Errorf("case %d: TOTP error exposes configuration or varies with its contents", i)
		}
	}
}
