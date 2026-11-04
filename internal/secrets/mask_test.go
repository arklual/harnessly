package secrets

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"net/url"
	"strings"
	"testing"
)

func TestMaskOverlappingValues(t *testing.T) {
	short := strings.Repeat("aB", 3)
	long := short + "/" + strings.Repeat("cD", 4)
	quoted, _ := json.Marshal(long)
	for _, value := range []string{long, url.QueryEscape(long), url.PathEscape(long), string(quoted[1 : len(quoted)-1])} {
		for _, values := range [][]string{{short, long}, {long, short}} {
			if got := Mask("before "+value+" after", values); got != "before [redacted] after" {
				t.Fatalf("overlapping mask: %q", got)
			}
		}
	}
	if got := Mask(short, []string{short, "redacted"}); got != "[redacted]" {
		t.Fatalf("replacement was rematched: %s", got)
	}
}

func TestMaskBrowserEncodings(t *testing.T) {
	value := "x*y z~!'()" + string([]rune{0x20ac, 0x26, 0x3c, 0x22})
	for _, encoded := range []string{
		"x*y+z%7E%21%27%28%29%E2%82%AC%26%3C%22",
		"x*y+z~%21%27%28%29%E2%82%AC%26%3C%22",
		"x*y%20z~!'()%E2%82%AC%26%3C%22",
		html.EscapeString(value), base64.StdEncoding.EncodeToString([]byte(value)),
		base64.RawStdEncoding.EncodeToString([]byte(value)), base64.URLEncoding.EncodeToString([]byte(value)),
		base64.RawURLEncoding.EncodeToString([]byte(value)),
	} {
		if Mask(encoded, []string{value}) != "[redacted]" {
			t.Fatal("browser encoding was not masked")
		}
		quoted, _ := json.Marshal(encoded)
		if Mask(string(quoted), []string{value}) != `"[redacted]"` {
			t.Fatal("JSON browser encoding was not masked")
		}
	}
}
