package designkit

// Version ranges for the kit record's extends: the small part of npm's syntax a kit needs.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type comparator struct {
	op  string // = > >= < <=
	ver string // major.minor.patch
}

// versionRange is comparators that must all hold (separated by spaces).
type versionRange []comparator

// parseRange reads "1.2.3", "1.2", "1.x", "*", "^1.2.3", "~1.2", ">=1.2.0 <2". "||" is not supported.
func parseRange(s string) (versionRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("the version range is empty")
	}
	if strings.Contains(s, "||") {
		return nil, errors.New("the version range does not support ||")
	}
	var out versionRange
	for _, tok := range strings.Fields(s) {
		cs, err := parseComparator(tok)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

func parseComparator(tok string) ([]comparator, error) {
	op := ""
	for _, p := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(tok, p) {
			op, tok = p, strings.TrimPrefix(tok, p)
			break
		}
	}
	tok = strings.TrimPrefix(tok, "v")
	core, _, _ := strings.Cut(tok, "-")
	if core == "*" || core == "x" || core == "X" {
		return nil, nil // any version
	}
	var n [3]int
	given := 0
	for i, part := range strings.Split(core, ".") {
		if i > 2 {
			return nil, fmt.Errorf("%q is not a version range", tok)
		}
		if part == "x" || part == "X" || part == "*" {
			break
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 || len(part) > 9 {
			return nil, fmt.Errorf("%q is not a version range", tok)
		}
		n[i], given = v, i+1
	}
	if given == 0 {
		return nil, fmt.Errorf("%q is not a version range", tok)
	}
	v := func(a, b, c int) string { return fmt.Sprintf("%d.%d.%d", a, b, c) }
	low := v(n[0], n[1], n[2])
	switch op {
	case "", "=":
		switch given {
		case 3:
			return []comparator{{"=", low}}, nil
		case 2:
			return []comparator{{">=", low}, {"<", v(n[0], n[1]+1, 0)}}, nil
		}
		return []comparator{{">=", low}, {"<", v(n[0]+1, 0, 0)}}, nil
	case "^":
		switch {
		case n[0] > 0 || given == 1:
			return []comparator{{">=", low}, {"<", v(n[0]+1, 0, 0)}}, nil
		case n[1] > 0 || given == 2:
			return []comparator{{">=", low}, {"<", v(0, n[1]+1, 0)}}, nil
		}
		return []comparator{{">=", low}, {"<", v(0, 0, n[2]+1)}}, nil
	case "~":
		if given == 1 {
			return []comparator{{">=", low}, {"<", v(n[0]+1, 0, 0)}}, nil
		}
		return []comparator{{">=", low}, {"<", v(n[0], n[1]+1, 0)}}, nil
	case ">":
		switch given {
		case 3:
			return []comparator{{">", low}}, nil
		case 2:
			return []comparator{{">=", v(n[0], n[1]+1, 0)}}, nil
		}
		return []comparator{{">=", v(n[0]+1, 0, 0)}}, nil
	case "<=":
		switch given {
		case 3:
			return []comparator{{"<=", low}}, nil
		case 2:
			return []comparator{{"<", v(n[0], n[1]+1, 0)}}, nil
		}
		return []comparator{{"<", v(n[0]+1, 0, 0)}}, nil
	}
	return []comparator{{op, low}}, nil // >= and <
}

// Satisfies is whether version (major.minor.patch with an optional pre-release) is in the range. A pre-release counts
// as the release it precedes, so 2.0.0-rc.1 is below 2.0.0 and inside ^1 only when it is a 1.x pre-release.
func (r versionRange) Satisfies(version string) bool {
	for _, c := range r {
		cmp := compareVersions(version, c.ver)
		ok := false
		switch c.op {
		case "=":
			ok = cmp == 0
		case ">":
			ok = cmp > 0
		case ">=":
			ok = cmp >= 0
		case "<":
			ok = cmp < 0 && !(preOf(version) != "" && stripPre(version) == c.ver)
		case "<=":
			ok = cmp <= 0
		}
		if !ok {
			return false
		}
	}
	return true
}

func preOf(v string) string { _, pre, _ := strings.Cut(v, "-"); return pre }
func stripPre(v string) string {
	main, _, _ := strings.Cut(v, "-")
	return main
}

// RangeSatisfies is whether version is in the range text, an error when the text is not a range.
func RangeSatisfies(rng, version string) (bool, error) {
	r, err := parseRange(rng)
	if err != nil {
		return false, err
	}
	return r.Satisfies(version), nil
}
