package main

import "github.com/ly-solutions/harnessly/internal/wire"

// homePlace is the folder of the machine's own place (wire.HomeConv): the projects' folder the deployment names, else
// the home folder; "" when there is neither.
func homePlace(d deps) string {
	if d.WorkspaceRoot != "" {
		return d.WorkspaceRoot
	}
	return d.Home
}

// homeRecord answers wire.HomeConv with the machine's own place (a record with only its folder), any other id as the
// conversations do. Without a folder the place is not there.
func homeRecord(root string, record func(string) (wire.Conversation, bool)) func(string) (wire.Conversation, bool) {
	return func(id string) (wire.Conversation, bool) {
		if id != wire.HomeConv {
			return record(id)
		}
		if root == "" {
			return wire.Conversation{}, false
		}
		return wire.Conversation{ID: id, Cwd: root}, true
	}
}
