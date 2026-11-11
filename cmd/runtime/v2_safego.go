package main

import (
	"fmt"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// A panic held at a goroutine's boundary goes to the runtime's own log, with the name of the work and its stack.
func init() {
	safego.SetLog(func(name string, p safego.Panic) {
		agent.Log("error", "panic held", agent.F{K: "in", V: name}, agent.F{K: "panic", V: fmt.Sprint(p.Value)},
			agent.F{K: "stack", V: string(p.Stack)}, agent.F{K: "held", V: safego.Panics()})
	})
}
