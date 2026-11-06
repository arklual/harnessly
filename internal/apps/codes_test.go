package apps

import (
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The error codes of the bridge are written out in three places that a view and a page read together: bridge.go (the
// runtime), web/app/next/features/app-views/bridge.ts (the page) and sdk/app-sdk.d.ts (what an app author types
// against). A code in one and not in the others would reach a view as a word its types do not know.

var codeConst = regexp.MustCompile(`(?m)^\s*Code\w+\s*=\s*"([a-z_]+)"`)
var quoted = regexp.MustCompile(`'([a-z_]+)'`)

func codesOf(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for _, m := range quoted.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func TestBridgeCodesAreTheSameInGoPageAndSDK(t *testing.T) {
	goSrc, err := os.ReadFile("bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	var goCodes []string
	for _, m := range codeConst.FindAllStringSubmatch(string(goSrc), -1) {
		goCodes = append(goCodes, m[1])
	}
	sort.Strings(goCodes)

	ts, err := os.ReadFile("../../web/app/next/features/app-views/bridge.ts")
	if err != nil {
		t.Fatal(err)
	}
	// The page's list is the array CODES = [ ... ].
	list := string(ts)[strings.Index(string(ts), "const CODES"):]
	list = list[:strings.Index(list, "];")]
	pageCodes := codesOf(t, list)

	dts, err := os.ReadFile("sdk/app-sdk.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	union := string(dts)[strings.Index(string(dts), "type HarnesslyErrorCode"):]
	union = union[:strings.Index(union, ";")]
	sdkCodes := codesOf(t, union)

	if len(goCodes) < 10 {
		t.Fatalf("read too few codes from bridge.go: %v", goCodes)
	}
	if !slices.Equal(goCodes, pageCodes) {
		t.Errorf("bridge.go %v\nbridge.ts %v", goCodes, pageCodes)
	}
	if !slices.Equal(goCodes, sdkCodes) {
		t.Errorf("bridge.go %v\napp-sdk.d.ts %v", goCodes, sdkCodes)
	}
}
