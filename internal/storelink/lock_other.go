//go:build !unix

package storelink

// lockFolder: without flock every runtime syncs the folder; identical writes are no-ops in the store.
func lockFolder(string, string) (func(), error) { return func() {}, nil }
