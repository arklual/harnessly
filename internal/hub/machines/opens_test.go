package machines

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestOpensRuntime(t *testing.T) {
	for _, c := range []struct {
		info string
		want bool
	}{
		{`{}`, true},
		{``, true},
		{`not json`, true},
		{`{"policy":{}}`, true},
		{`{"policy":{"runtimeSession":true}}`, true},
		{`{"policy":{"runtimeSession":false,"source":"observe-only"}}`, false},
	} {
		if got := opensRuntime(store.Machine{Info: c.info}); got != c.want {
			t.Errorf("%q: got %v", c.info, got)
		}
	}
}
