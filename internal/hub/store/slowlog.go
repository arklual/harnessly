package store

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"time"
)

// slowQuery is how long a query may take before it is logged with its caller, so the slow screens of the hub can be
// traced to the query behind them without a tracer.
const slowQuery = 200 * time.Millisecond

// logSlow logs q when it ran slowQuery or longer since start: the query squeezed to one line and the two functions
// above the database (the typed query and what asked for it).
func logSlow(ctx context.Context, q string, start time.Time) {
	d := time.Since(start)
	if d < slowQuery {
		return
	}
	slog.WarnContext(ctx, "hub slow query", "ms", d.Milliseconds(), "caller", callers(3, 2), "sql", squeeze(q, 400))
}

func callers(skip, n int) string {
	pc := make([]uintptr, n)
	got := runtime.Callers(skip+1, pc)
	frames := runtime.CallersFrames(pc[:got])
	var out []string
	for {
		f, more := frames.Next()
		name := f.Function
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		out = append(out, name)
		if !more {
			break
		}
	}
	return strings.Join(out, " < ")
}

func squeeze(q string, max int) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > max {
		q = q[:max] + "…"
	}
	return q
}
