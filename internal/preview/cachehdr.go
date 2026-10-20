package preview

import (
	"math"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

// What a slot response may be cached as. A slot origin is one person's: no shared cache may keep or hand on what it
// answered, so `private` is always there and `public`/`s-maxage` never. The upstream's own freshness stays for static
// files; a document, where the bridge is injected, is always revalidated, and so is an API answer that names no
// freshness.

var (
	directiveRe  = regexp.MustCompile(`(?i)^\s*([a-z-]+)(?:\s*=\s*"?([^",]*)"?)?\s*$`)
	dynamicTypes = regexp.MustCompile(`(?i)^(application/(json|[a-z.+-]*\+json|x-ndjson|ndjson|xml|graphql-response\+json)|text/(event-stream|xml)|[a-z]+/[a-z.+-]*\+(json|xml))\b`)
)

type directive struct {
	name  string
	value string
	has   bool
}

func parseDirectives(v string) []directive {
	var out []directive
	for _, part := range strings.Split(v, ",") {
		m := directiveRe.FindStringSubmatchIndex(part)
		if m == nil {
			continue
		}
		d := directive{name: strings.ToLower(part[m[2]:m[3]])}
		if m[4] >= 0 {
			d.value, d.has = part[m[4]:m[5]], true
		}
		out = append(out, d)
	}
	return out
}

func (d directive) String() string {
	if !d.has {
		return d.name
	}
	return d.name + "=" + d.value
}

func slotCacheControl(h Headers, document bool) string {
	ds := parseDirectives(h.Get("cache-control"))
	names := map[string]bool{}
	for _, d := range ds {
		names[d.name] = true
	}
	if names["no-store"] {
		parts := make([]string, len(ds))
		for i, d := range ds {
			parts[i] = d.String()
		}
		return strings.Join(parts, ", ")
	}
	explicit := names["max-age"] || h.Has("expires")
	revalidate := document || dynamicTypes.MatchString(h.Get("content-type")) && !explicit
	out := []string{"private"}
	noRevalidation := false
	for _, d := range ds {
		switch d.name {
		case "public", "s-maxage", "private", "proxy-revalidate":
			continue
		case "max-age", "immutable", "stale-while-revalidate", "stale-if-error":
			if revalidate {
				continue
			}
		}
		if d.name == "no-store" || d.name == "no-cache" {
			noRevalidation = true
		}
		out = append(out, d.String())
	}
	if revalidate && !noRevalidation {
		out = append(out, "no-cache")
	}
	return strings.Join(out, ", ")
}

// markDocument: a document with the bridge in it; whatever validators the upstream gave describe another body.
func markDocument(h Headers) {
	h.Del("etag")
	h.Del("last-modified")
	h.Del("expires")
}

type freshness struct {
	maxAge    float64
	hasMaxAge bool
	immutable bool
	forbidden bool
}

func freshnessOf(v string) freshness {
	var f freshness
	seenMaxAge := false
	for _, d := range parseDirectives(v) {
		switch d.name {
		case "max-age":
			if !seenMaxAge && d.has {
				if n := devtools.JSNumber(d.value); !math.IsInf(n, 0) && !math.IsNaN(n) {
					f.maxAge, f.hasMaxAge = n, true
				}
			}
			seenMaxAge = true
		case "immutable":
			f.immutable = true
		case "no-store", "no-cache", "private":
			f.forbidden = true
		}
	}
	return f
}
