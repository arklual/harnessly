package heavy

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrBusy is Hold's answer when no slot freed up within the wait.
var ErrBusy = errBusy

// Hold takes one of the machine's shared slots for work the caller runs itself (a draft render of a 3D scene), waiting up
// to wait for one. The returned function frees the slot; call it once the work is over. When the slots cannot be kept at
// all (a read-only folder) the work is let through unheld, as the launcher does. A slot is one of the same flocks the
// launchers take, so a draft render waits for builds and tests and they for it.
func Hold(tool string, wait time.Duration) (release func(), err error) {
	env := OSEnv()
	env.fill()
	dir := Dir(env.Getenv)
	n := Slots(env.CPUs)
	if v, err := strconv.Atoi(strings.TrimSpace(env.Getenv(envSlots))); err == nil && v > 0 {
		n = v
	}
	start := env.Now()
	_, lock, err := acquire(dir, n, wait, env, func() {})
	waited := env.Now().Sub(start)
	switch {
	case errors.Is(err, errBusy):
		record(dir, env.Now(), tool, "refused", waited)
		return func() {}, ErrBusy
	case err != nil:
		return func() {}, nil
	}
	outcome := "ran"
	if waited >= env.Poll {
		outcome = "waited"
	}
	record(dir, env.Now(), tool, outcome, waited)
	return func() {
		if lock != nil {
			_ = lock.Close()
		}
	}, nil
}

// Gentle is the argv with nice and ionice put before it when the machine has them.
func Gentle(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	return gentle(os.Environ(), argv, argv[0])
}
