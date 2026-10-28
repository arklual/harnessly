//go:build !(linux || darwin)

package agent

// fileWritableByAgent: without Unix permissions there is no telling who can write the policy, so it is taken as writable
// by the agent's account (a change that allows more waits for SIGHUP or a restart).
func fileWritableByAgent(string) bool { return true }
