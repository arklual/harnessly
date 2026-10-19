package machines

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A week of history at one line a minute per provider, the most the writer keeps.
func benchData(b *testing.B) (string, time.Time) {
	dir := b.TempDir()
	now := time.Unix(1792315800, 0)
	var lines []string
	n := now.Unix()
	for t := n - 8*86400; t < n; t += 60 {
		for _, p := range []string{"claude", "codex"} {
			lines = append(lines, fmt.Sprintf(`{"t":%d,"p":"%s","w":{"five_hour":[%d,%d,%d],"seven_day":[%d,%d,%d],"opus":[3,%d,%d]}}`,
				t, p, (t/600)%100, n+3600, t, (t/6000)%100, n+86400, t, n+86400, t))
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "limits-history.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	w := func(u int, r int64) string {
		return fmt.Sprintf(`{"used_percentage":%d,"resets_at":%d,"at":%d}`, u, r, n-10)
	}
	raw := fmt.Sprintf(`{"claude":{"ts":%d,"five_hour":%s,"seven_day":%s,"extra":[{"id":"opus","title":"Opus","window":%s}]},"codex":{"ts":%d,"five_hour":%s,"seven_day":%s}}`,
		n-5, w(40, n+3600), w(60, n+86400), w(3, n+86400), n-5, w(10, n+3600), w(20, n+86400))
	_ = os.WriteFile(filepath.Join(dir, "rate-limits.json"), []byte(raw), 0o600)
	return dir, now
}

func BenchmarkMachineSnapshot(b *testing.B) {
	dir, now := benchData(b)
	var events [][2]float64
	for t := float64(now.Unix()) - 40*86400; t < float64(now.Unix()); t += 300 {
		events = append(events, [2]float64{t, 0.3})
	}
	act := map[string]*Activity{"claude": BuildActivity(events, float64(now.Unix())-60, 3*3600), "codex": BuildActivity(events, float64(now.Unix())-60, 3*3600)}
	s := NewSnapshot(SnapshotConfig{DataDir: dir, User: "agent", Now: func() time.Time { return now },
		Activity: func() map[string]*Activity { return act }})
	s.Machine()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Machine()
	}
}
