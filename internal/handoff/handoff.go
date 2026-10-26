// Package handoff carries a conversation's context to another one: a fork's own copy of a Claude session cut after a
// turn, an excerpt of a transcript within a budget, the note an agent writes for the next one, and the blocks these go
// in as a first message (a handoff) or as a message back into the conversation a fork came from (a merge).
package handoff

import (
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// DefaultBudget is how many tokens an excerpt takes at most unless the target's window is smaller.
const DefaultBudget = 16_000

// Tokens is the size of a text as the runtime estimates it: about four characters a token.
func Tokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }

// Window is the context window of a model, in tokens, as far as the runtime knows it.
func Window(provider wire.Provider, model string) int {
	switch {
	case provider == wire.ProviderClaude && strings.HasSuffix(model, "[1m]"):
		return 1_000_000
	case provider == wire.ProviderClaude:
		return 200_000
	}
	return 272_000
}

// Budget is the tokens an excerpt for the model may take: DefaultBudget, and never more than a quarter of its window.
func Budget(provider wire.Provider, model string) int {
	return BudgetIn(Window(provider, model))
}

// BudgetIn is Budget for a model whose window is known: the vendors' models on a backend profile have their own.
func BudgetIn(window int) int {
	return min(DefaultBudget, window/4)
}

// Agent is how the blocks name an agent.
func Agent(p wire.Provider) string {
	if p == wire.ProviderCodex {
		return "Codex"
	}
	return "Claude"
}

// From is the conversation a block comes from.
type From struct {
	ID, Title string
	Provider  wire.Provider
	Model     string
}

// The blocks' tags. The page knows them too (web/app/next/features/chat/handoff.ts): a message that is one of them is
// drawn as a card, not as its markup.
const (
	tagHandoff = "handoff"
	tagResult  = "fork-result"
)

var attrEscaper = strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;", "\n", " ", "\r", " ")

func attr(k, v string) string { return " " + k + `="` + attrEscaper.Replace(v) + `"` }

// keepInside breaks a closing tag of the block in the text, so the text cannot end the block early.
func keepInside(text, tag string) string {
	return strings.ReplaceAll(text, "</"+tag, `<\/`+tag)
}

func block(tag, attrs, instructions, text string) string {
	return "<" + tag + attrs + ">\n<instructions>" + instructions + "</instructions>\n\n" +
		strings.TrimSpace(keepInside(text, tag)) + "\n</" + tag + ">"
}

// Handoff is the first message of a conversation that takes over another one's context: the text (an excerpt or a
// note) in a block that says where it comes from and what the agent is to do with it. fork marks the excerpt a fork
// starts with where the agent could not fork its session.
func Handoff(from From, mode wire.HandoffMode, fork bool, text string) string {
	attrs := attr("from", from.ID) + attr("title", from.Title) + attr("agent", string(from.Provider)) +
		attr("model", from.Model) + attr("mode", string(mode))
	what := "a note the agent there wrote for you"
	if mode == wire.HandoffExcerpt {
		what = "an excerpt of its transcript: the person's first request, the latest turns and the results of key commands"
	}
	who := Agent(from.Provider)
	if from.Model != "" {
		who += " (" + from.Model + ")"
	}
	lead := "This conversation continues the work of another one, where " + who + " worked"
	if fork {
		attrs += attr("kind", "fork")
		lead = "This conversation is a branch of another one, where " + who + " worked"
	}
	instructions := lead + ". Below is its context: " + what + ". The folder is the same. Read it, say in a sentence " +
		"or two what you took over, and wait for the person's next message; do not redo what is done."
	return block(tagHandoff, attrs, instructions, text)
}

// Result is the message a fork sends back into the conversation it came from: the result note in a block that says
// which branch it is.
func Result(fork From, text string) string {
	attrs := attr("from", fork.ID) + attr("title", fork.Title) + attr("agent", string(fork.Provider))
	instructions := "This is the result of a branch of this conversation, done in a separate chat. Take it into " +
		"account from now on: say in a sentence or two what it changes for the work here, and wait for the person."
	return block(tagResult, attrs, instructions, text)
}

// NotePrompt is what the source agent is asked to write the handoff note with.
const NotePrompt = "Write a handoff note for another agent that will continue this work in a new conversation. " +
	"It has no access to this conversation: only to the same folder. Use exactly these Markdown sections: " +
	"## Goal, ## Decisions, ## Current state, ## Next steps, ## Files, ## Verify, ## Gotchas. " +
	"Be concrete: paths, commands, ids, what passed and what failed. Leave out what does not matter for the next steps. " +
	"No preamble and no closing words. Write in the language the person writes in."

// ResultPrompt is what a fork's agent is asked to write the note it sends back with.
const ResultPrompt = "This chat is a branch of another conversation. Write a short result note for that conversation: " +
	"what was done here, the files touched, the decisions made and what is left. Markdown, at most about 25 lines. " +
	"No preamble and no closing words. Write in the language the person writes in."

// PromptOf is the prompt that writes the draft of a mode that needs the agent; "" for an excerpt.
func PromptOf(mode wire.DraftMode) string {
	switch mode {
	case wire.DraftNote:
		return NotePrompt
	case wire.DraftResult:
		return ResultPrompt
	}
	return ""
}
