// Package updatecheck tells a self-hosted runtime whether a newer release is published: a periodic GET of the
// project's latest-release address, compared with the running version. It sends nothing but that request.
package updatecheck

import (
	"strconv"
	"strings"
)

// Version is a parsed semantic version: major.minor.patch with an optional pre-release (build metadata is dropped).
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// Parse reads "1.2.3", "v1.2.3" or "1.2.3-rc.1"; ok is false for anything else (including "dev").
func Parse(s string) (v Version, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.Pre = s[i+1:]
		s = s[:i]
		if v.Pre == "" {
			return Version{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return Version{}, false
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	return v, true
}

func (v Version) String() string {
	s := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Compare returns -1, 0 or 1. A pre-release is older than its release.
func Compare(a, b Version) int {
	for _, d := range [3]int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	return comparePre(a.Pre, b.Pre)
}

// comparePre follows semver: dot-separated identifiers, numbers numerically and below words, a shorter list first.
func comparePre(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		m, em := strconv.Atoi(x[i])
		n, en := strconv.Atoi(y[i])
		switch {
		case em == nil && en == nil:
			if m < n {
				return -1
			}
			return 1
		case em == nil:
			return -1
		case en == nil:
			return 1
		case x[i] < y[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(x) < len(y):
		return -1
	case len(x) > len(y):
		return 1
	}
	return 0
}
