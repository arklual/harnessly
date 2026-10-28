// Package docsecret finds a secret in a document before it leaves the machine. The shapes are the exact token
// formats the cluster cuts out of transcripts (the redaction of an internal service); there is no entropy
// rule, so ordinary hashes and ids are not taken for secrets. A document is not cut silently: it is refused, with the
// line of the first token, so the person removes it knowingly.
package docsecret

import (
	"regexp"
	"sort"
)

// Hit is one found secret: the line it starts on (from 1) and the kind of token.
type Hit struct {
	Line int
	Kind string
}

var rules = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"jwt", regexp.MustCompile(`\beyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{10,}`)},
	{"anthropic", regexp.MustCompile(`\bsk-ant-[\w-]{20,}`)},
	{"openai", regexp.MustCompile(`\bsk-(?:proj-)?[\w-]{20,}`)},
	{"github", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_\w{22,})`)},
	{"gitlab", regexp.MustCompile(`\bglpat-[\w-]{20,}`)},
	{"youtrack", regexp.MustCompile(`\bperm[:-][A-Za-z0-9=_+/-]{4,}\.[A-Za-z0-9=_+/-]{4,}\.[A-Za-z0-9=_+/-]{10,}`)},
	{"vault", regexp.MustCompile(`\bhvs\.[A-Za-z0-9_-]{24,}`)},
	{"slack", regexp.MustCompile(`\bxox[abprs]-[\w-]{10,}`)},
	{"telegram", regexp.MustCompile(`\b\d{8,10}:[\w-]{35}`)},
	{"aws", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"google", regexp.MustCompile(`\bAIza[\w-]{35}`)},
	{"bearer", regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/=-]{24,}`)},
	{"url-credentials", regexp.MustCompile(`(?i)\bhttps?://[^\s:/@]+:[^\s@/]+@`)},
	{"conn-string", regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|rediss?|amqps?)://[^:\s/@]+:[^@\s]+@`)},
}

// Find returns the first secret of text by position, or false when there is none.
func Find(text string) (Hit, bool) {
	type found struct {
		at   int
		kind string
	}
	var hits []found
	for _, rule := range rules {
		if loc := rule.re.FindStringIndex(text); loc != nil {
			hits = append(hits, found{loc[0], rule.kind})
		}
	}
	if len(hits) == 0 {
		return Hit{}, false
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	line := 1
	for i := 0; i < hits[0].at; i++ {
		if text[i] == '\n' {
			line++
		}
	}
	return Hit{Line: line, Kind: hits[0].kind}, true
}
