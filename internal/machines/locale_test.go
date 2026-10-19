package machines

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestForecastReasonsInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if f := ForecastWindow(nil, 1000, FiveHour, nil); f.Reason == nil || *f.Reason != "no samples" {
		t.Errorf("empty: %+v", f)
	}
	now := 100_000.0
	f := ForecastWindow(samplesOf([][3]float64{{now - 60, 10, now + 3600}, {now - 30, 11, now + 3600}}), now, FiveHour, nil)
	if f.Reason == nil || *f.Reason != "needs at least 20 min of samples" {
		t.Errorf("short: %+v", f)
	}
	for sec, want := range map[float64]string{1200: "20 min", 6 * 3600: "6 h", 5400: "1 h 30 min"} {
		if got := formatSpan(sec); got != want {
			t.Errorf("%v: %q, want %q", sec, got, want)
		}
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := formatSpan(5400); got != "1 ч 30 мин" {
		t.Errorf("ru: %q", got)
	}
}
