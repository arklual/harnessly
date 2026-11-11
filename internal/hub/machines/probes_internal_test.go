package machines

import (
	"strconv"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// ProbeValid is probeValid for the tests of the package's API.
const ProbeValid = probeValid

func TestProbesFresh(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	info := func(at time.Time, version string) store.Machine {
		return store.Machine{Info: `{"preflight":{"osName":"x"},"agents":[{"provider":"claude"}],"probed":{"at":` +
			ms(at.UnixMilli()) + `,"agentVersion":"` + version + `"}}`}
	}
	for _, c := range []struct {
		name string
		m    store.Machine
		want bool
	}{
		{"probed a minute ago by this agent", info(now.Add(-time.Minute), "1.0.0"), true},
		{"probed before the interval", info(now.Add(-probeValid), "1.0.0"), false},
		{"probed by another version of the agent", info(now.Add(-time.Minute), "0.9.0"), false},
		{"a mark from the future", info(now.Add(time.Minute), "1.0.0"), false},
		{"never probed", store.Machine{Info: `{"preflight":{"osName":"x"},"agents":[]}`}, false},
		{"a mark without the reports", store.Machine{Info: `{"probed":{"at":` + ms(now.UnixMilli()) + `,"agentVersion":"1.0.0"}}`}, false},
		{"nothing remembered", store.Machine{Info: `{}`}, false},
	} {
		if got := probesFresh(c.m, "1.0.0", now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func ms(v int64) string { return strconv.FormatInt(v, 10) }
