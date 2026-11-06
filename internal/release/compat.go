package release

import (
	"fmt"
	"regexp"
	"strconv"
)

// An extension protocol is `harnessly.<kind>/v<major>` (internal/hub/ext); a manifest names the family without the
// major: `harnessly.<kind>`.
var (
	protoName = regexp.MustCompile(`^harnessly\.[a-z][a-z0-9-]*$`)
	protoFull = regexp.MustCompile(`^(harnessly\.[a-z][a-z0-9-]*)/v([1-9][0-9]*)$`)
)

// Verdict of one registered extension against a release.
type Verdict string

const (
	// Compatible: the release speaks the extension's protocol major.
	Compatible Verdict = "compatible"
	// Incompatible: the release speaks that protocol, but not that major.
	Incompatible Verdict = "incompatible"
	// Unknown: the release does not mention the protocol; nothing is promised either way.
	Unknown Verdict = "unknown"
)

// ExtensionCheck is one line of the plan.
type ExtensionCheck struct {
	Name     string  `json:"name"`
	Protocol string  `json:"protocol"`
	Verdict  Verdict `json:"verdict"`
	Detail   string  `json:"detail"`
}

// CheckExtension compares the protocol an extension is registered with to the release's ranges.
func (m *Manifest) CheckExtension(name, protocol string) ExtensionCheck {
	c := ExtensionCheck{Name: name, Protocol: protocol}
	g := protoFull.FindStringSubmatch(protocol)
	if g == nil {
		c.Verdict, c.Detail = Incompatible, "the protocol is not harnessly.<kind>/v<N>"
		return c
	}
	major, _ := strconv.Atoi(g[2])
	r, ok := m.ExtensionAPI[g[1]]
	switch {
	case !ok:
		c.Verdict, c.Detail = Unknown, fmt.Sprintf("release %s does not list %s", m.Version, g[1])
	case major < r.Min || major > r.Max:
		c.Verdict, c.Detail = Incompatible, fmt.Sprintf("release %s speaks %s v%d..v%d, the extension is v%d", m.Version, g[1], r.Min, r.Max, major)
	default:
		c.Verdict, c.Detail = Compatible, fmt.Sprintf("release %s speaks %s v%d..v%d", m.Version, g[1], r.Min, r.Max)
	}
	return c
}
