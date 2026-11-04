package secrets

import (
	"encoding/json"
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
