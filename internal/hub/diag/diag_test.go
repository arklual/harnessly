package diag

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDescribe(t *testing.T) {
	p := &Problem{Code: PortBusy, What: "listening on :80", Err: errors.New("address in use"), Fix: "change HUB_LISTEN"}
	out := Describe(fmt.Errorf("wrapped: %w", p))
	for _, want := range []string{"HUB-X002", "listening on :80: address in use", "what to do: change HUB_LISTEN", "docs/hub/doctor.md#hub-x002"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q lacks %q", out, want)
		}
	}
	if out := Describe(errors.New("boom")); !strings.Contains(out, StartFailed) || !strings.Contains(out, "boom") {
		t.Errorf("a plain error: %q", out)
	}
}
