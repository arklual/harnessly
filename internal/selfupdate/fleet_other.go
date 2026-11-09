//go:build !unix

package selfupdate

// lockRoot: no advisory lock off Unix; a rollout there is one at a time by the caller.
func lockRoot(string) (func(), error) { return func() {}, nil }
