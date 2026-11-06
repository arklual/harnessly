package apps

import (
	"fmt"
	"strconv"
	"strings"
)

// The host versions an app works with: comparators joined by spaces (all must hold), alternatives by "||". A comparator
// is >=, >, <=, <, = or nothing before a version, ^x.y (same major; same minor below 1.0), ~x.y (same minor) or *.
// Missing parts are zeros: >=0.17 is >=0.17.0. A pre-release suffix is ignored.

// Range is a parsed harnessly field.
type Range [][]comparator

type comparator struct {
	op string
	v  [3]int
}

// ParseRange reads a range; the error says what is wrong in words.
func ParseRange(s string) (Range, error) {
	var r Range
	for _, alt := range strings.Split(s, "||") {
		fields := strings.Fields(alt)
		if len(fields) == 0 {
			return nil, fmt.Errorf("%q has an empty alternative", s)
		}
		var set []comparator
		for _, f := range fields {
			cs, err := parseComparator(f)
			if err != nil {
				return nil, fmt.Errorf("%q: %v", s, err)
			}
			set = append(set, cs...)
		}
		r = append(r, set)
	}
	return r, nil
}

func parseComparator(f string) ([]comparator, error) {
	if f == "*" || f == "x" {
		return nil, nil
	}
	for _, op := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		rest, ok := strings.CutPrefix(f, op)
		if !ok {
			continue
		}
		v, parts, err := parseVersion(rest)
		if err != nil {
			return nil, err
		}
		switch op {
		case "^":
			hi := [3]int{v[0] + 1, 0, 0}
			if v[0] == 0 {
				hi = [3]int{0, v[1] + 1, 0}
			}
			return []comparator{{">=", v}, {"<", hi}}, nil
		case "~":
			if parts == 1 {
				return []comparator{{">=", v}, {"<", [3]int{v[0] + 1, 0, 0}}}, nil
			}
			return []comparator{{">=", v}, {"<", [3]int{v[0], v[1] + 1, 0}}}, nil
		}
		return []comparator{{op, v}}, nil
	}
	v, _, err := parseVersion(f)
	if err != nil {
		return nil, err
	}
	return []comparator{{"=", v}}, nil
}

// parseVersion reads 1, 1.2 or 1.2.3 (a pre-release suffix dropped) and says how many parts were written.
func parseVersion(s string) ([3]int, int, error) {
	var v [3]int
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 3 {
		return v, 0, fmt.Errorf("%q is not a version", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, 0, fmt.Errorf("%q is not a version", s)
		}
		v[i] = n
	}
	return v, len(parts), nil
}

// Allows says whether the host version is in the range. A host built from source ("dev") is in every range.
func (r Range) Allows(version string) bool {
	if version == "dev" || version == "" {
		return true
	}
	v, _, err := parseVersion(version)
	if err != nil {
		return false
	}
	for _, set := range r {
		ok := true
		for _, c := range set {
			if !c.holds(v) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (c comparator) holds(v [3]int) bool {
	d := compare(v, c.v)
	switch c.op {
	case ">=":
		return d >= 0
	case ">":
		return d > 0
	case "<=":
		return d <= 0
	case "<":
		return d < 0
	}
	return d == 0
}

func compare(a, b [3]int) int {
	for i := range 3 {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// CompareVersions orders two app versions (1.2.0 < 1.10.0); a version that does not parse sorts first.
func CompareVersions(a, b string) int {
	va, _, ea := parseVersion(a)
	vb, _, eb := parseVersion(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	}
	return compare(va, vb)
}

// lowest is the least version the range asks for ("1.2.0"), "" when no alternative bounds it from below.
func (r Range) lowest() string {
	var best [3]int
	found := false
	for _, set := range r {
		var low [3]int
		bounded := false
		for _, c := range set {
			if c.op == ">=" || c.op == ">" || c.op == "=" {
				if !bounded || compare(c.v, low) > 0 {
					low, bounded = c.v, true
				}
			}
		}
		if !bounded {
			return ""
		}
		if !found || compare(low, best) < 0 {
			best, found = low, true
		}
	}
	if !found {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", best[0], best[1], best[2])
}
