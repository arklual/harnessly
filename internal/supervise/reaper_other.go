//go:build !unix

package supervise

// Without Unix wait semantics there is nothing to reap: the supervisor waits for its children itself.
type reaper struct{}

func startReaper() *reaper                  { return nil }
func (r *reaper) stop()                     {}
func (r *reaper) watch(int) chan waitResult { return nil }
