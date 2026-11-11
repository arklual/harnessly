package main

import "github.com/ly-solutions/harnessly/internal/sources"

// paneFeedEnv set to 0 keeps the conversations' channels without what the page's right pane reads of a chat: the page
// then reads the conversation for its pane by itself, over the timeline socket, as before the runtime gathered it.
// Anything else, or nothing, sends it.
const paneFeedEnv = "RUNTIME_PANE_FEED"

// paneLinks is how the links of the pane's data are told apart; nil when the data is switched off.
func paneLinks(getenv func(string) string) *sources.Links {
	if getenv(paneFeedEnv) == "0" {
		return nil
	}
	return sourceLinks()
}
