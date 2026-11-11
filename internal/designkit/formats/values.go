package formats

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	aliasRe = regexp.MustCompile(`^\{([^{}]+)\}$`)
	hexRe   = regexp.MustCompile(`(?i)^#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$`)
	lenRe   = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)(px|rem|em|%|pt|vh|vw)?$`)
	// A colour the kit's readers take: hex, rgb(), hsl(), oklch() (the same words as the check's contract).
	colorFnRe = regexp.MustCompile(`(?i)^(rgba?|hsla?|oklch|oklab|lab|lch|hwb|color)\(`)
)

// looksLikeColor is whether a CSS value is a colour a kit can hold.
func looksLikeColor(v string) bool {
	v = strings.TrimSpace(v)
	if unsafeCSS(v) {
		return false
	}
	return hexRe.MatchString(v) || colorFnRe.MatchString(v)
}

// splitTop splits on sep outside parentheses and quotes.
func splitTop(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote && s[i-1] != '\\' {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(s[start:]))
}

// splitSpaces splits a CSS value on blanks outside parentheses.
func splitSpaces(s string) []string {
	var parts []string
	depth, start := 0, -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		}
		blank := c == ' ' || c == '\t' || c == '\n'
		if blank && depth == 0 {
			if start >= 0 {
				parts = append(parts, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		parts = append(parts, s[start:])
	}
	return parts
}

// shadowLayers reads a CSS box-shadow value as DTCG shadow objects; ok is false when a layer is not
// [inset] <x> <y> [<blur> [<spread>]] <colour> in either order.
func shadowLayers(css string) ([]map[string]any, bool) {
	var out []map[string]any
	for _, layer := range splitTop(css, ',') {
		var lens []string
		color, inset := "", false
		for _, p := range splitSpaces(layer) {
			switch {
			case strings.EqualFold(p, "inset"):
				inset = true
			case lenRe.MatchString(p):
				lens = append(lens, p)
			case color == "":
				color = p
			default:
				return nil, false
			}
		}
		if len(lens) < 2 || len(lens) > 4 || color == "" {
			return nil, false
		}
		for len(lens) < 4 {
			lens = append(lens, "0")
		}
		l := map[string]any{"color": color, "offsetX": lens[0], "offsetY": lens[1], "blur": lens[2], "spread": lens[3]}
		if inset {
			l["inset"] = true
		}
		out = append(out, l)
	}
	return out, len(out) > 0
}

// shadowCSS is the inverse of shadowLayers.
func shadowCSS(raw json.RawMessage) (string, bool) {
	if s, ok := jsonString(raw); ok {
		return s, s != "" // a CSS text kept as it was (a value that is not a layer list)
	}
	var layers []map[string]any
	if json.Unmarshal(raw, &layers) != nil {
		var one map[string]any
		if json.Unmarshal(raw, &one) != nil {
			return "", false
		}
		layers = []map[string]any{one}
	}
	var parts []string
	for _, l := range layers {
		c, ok := l["color"]
		if !ok {
			return "", false
		}
		color, ok := dtcgColorAny(c)
		if !ok {
			return "", false
		}
		var segs []string
		if in, _ := l["inset"].(bool); in {
			segs = append(segs, "inset")
		}
		dims := make([]string, 4)
		for i, k := range []string{"offsetX", "offsetY", "blur", "spread"} {
			d, ok := dimensionAny(l[k])
			if !ok || zeroLen(d) {
				d = "0"
			}
			dims[i] = d
		}
		if dims[3] == "0" { // a zero spread is the default
			dims = dims[:3]
			if dims[2] == "0" {
				dims = dims[:2]
			}
		}
		segs = append(segs, dims...)
		parts = append(parts, strings.Join(append(segs, color), " "))
	}
	return strings.Join(parts, ", "), len(parts) > 0
}

// dimensionAny reads a DTCG dimension: "8px", 8 or {"value": 8, "unit": "px"}.
func dimensionAny(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, x != ""
	case float64:
		if x == 0 {
			return "0", true
		}
		return trimFloat(x) + "px", true
	case map[string]any:
		n, ok := x["value"].(float64)
		unit, _ := x["unit"].(string)
		if !ok {
			return "", false
		}
		if n == 0 {
			return "0", true
		}
		return trimFloat(n) + unit, true
	}
	return "", false
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(math.Round(f*10000)/10000, 'f', -1, 64)
}

func dimensionRaw(raw json.RawMessage) (string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return dimensionAny(v)
}

// dtcgColorAny reads a DTCG colour: a CSS string, or the object form {colorSpace, components, alpha, hex}.
func dtcgColorAny(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, x != "" && !unsafeCSS(x)
	case map[string]any:
		alpha := 1.0
		if a, ok := x["alpha"].(float64); ok {
			alpha = a
		}
		space, _ := x["colorSpace"].(string)
		comps, _ := x["components"].([]any)
		nums := make([]float64, 0, 3)
		for _, c := range comps {
			f, ok := c.(float64)
			if !ok {
				return "", false // "none" components
			}
			nums = append(nums, f)
		}
		if h, ok := x["hex"].(string); ok && alpha == 1 && hexRe.MatchString(h) {
			return h, true
		}
		if len(nums) != 3 {
			return "", false
		}
		switch space {
		case "srgb", "":
			return rgbaString(nums[0], nums[1], nums[2], alpha), true
		case "hsl":
			if alpha == 1 {
				return fmt.Sprintf("hsl(%s %s%% %s%%)", trimFloat(nums[0]), trimFloat(nums[1]), trimFloat(nums[2])), true
			}
			return fmt.Sprintf("hsl(%s %s%% %s%% / %s)", trimFloat(nums[0]), trimFloat(nums[1]), trimFloat(nums[2]), trimFloat(alpha)), true
		case "oklch":
			if alpha == 1 {
				return fmt.Sprintf("oklch(%s %s %s)", trimFloat(nums[0]), trimFloat(nums[1]), trimFloat(nums[2])), true
			}
			return fmt.Sprintf("oklch(%s %s %s / %s)", trimFloat(nums[0]), trimFloat(nums[1]), trimFloat(nums[2]), trimFloat(alpha)), true
		}
	}
	return "", false
}

// rgbaString turns 0..1 channels into #rrggbb (opaque) or rgb(r g b / a).
func rgbaString(r, g, b, a float64) string {
	c := func(f float64) int { return int(math.Round(math.Max(0, math.Min(1, f)) * 255)) }
	if a >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", c(r), c(g), c(b))
	}
	return fmt.Sprintf("rgb(%d %d %d / %s)", c(r), c(g), c(b), trimFloat(math.Round(a*1000)/1000))
}

// sanitizeName makes a token name the check accepts: letters, digits, _ . -, starting with a letter or digit.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func zeroLen(s string) bool {
	s = strings.TrimSpace(s)
	for _, u := range []string{"px", "rem", "em", "%"} {
		s = strings.TrimSuffix(s, u)
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f == 0
}

// unsafeCSS is whether a value could end its declaration or rule when written into a stylesheet.
func unsafeCSS(v string) bool {
	return strings.ContainsAny(v, ";{}<>\n\r") || strings.Contains(v, "/*") || strings.Contains(v, "*/")
}

// safeModeID makes a mode id fit for a selector and a JS string.
func safeModeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
