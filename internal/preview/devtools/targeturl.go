package devtools

import (
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// NeighbourPrefix is the path of the main slot under which neighbour ports (MFE remotes) are served.
const NeighbourPrefix = "/__tk/p/"

func neighbourEnd(c rune) bool {
	return strings.ContainsRune("/?#:'\")", c) || jscompat.IsSpace(c)
}

// neighbourAt is the port of "/__tk/p/<port>" at the start of s when the address ends there, and its length.
func neighbourAt(s string) (string, int) {
	if !strings.HasPrefix(s, NeighbourPrefix) {
		return "", 0
	}
	rest := s[len(NeighbourPrefix):]
	if rest == "" || rest[0] < '1' || rest[0] > '9' {
		return "", 0
	}
	n := 1
	for n < len(rest) && n < 5 && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n < len(rest) {
		if r, _ := utf8.DecodeRuneInString(rest[n:]); !neighbourEnd(r) {
			return "", 0
		}
	}
	return rest[:n], len(NeighbourPrefix) + n
}

// ToTargetText turns every slot address in a text (a URL, a message, a stack trace) into the target's:
// "<slot>/__tk/p/5173/x" is the neighbour's "http://localhost:5173/x". A page's report speaks of the slot; people and
// agents know the target.
func ToTargetText(text, slot string, target *Target) string {
	if target == nil || !strings.Contains(text, slot) {
		return text
	}
	own := target.TargetOrigin()
	var b strings.Builder
	for {
		i := strings.Index(text, slot)
		if i < 0 || slot == "" {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		text = text[i+len(slot):]
		port, n := neighbourAt(text)
		switch {
		case n == 0:
			b.WriteString(own)
		case target.Kind == "port":
			b.WriteString("http://localhost:" + port)
		default:
			b.WriteString(own + text[:n])
		}
		text = text[n:]
	}
}
