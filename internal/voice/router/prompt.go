package router

import (
	"fmt"
	"strings"
)

// Instructions is what the fast model is told once: who it is and the plan's form.
const Instructions = `You are the hands of a voice assistant inside harnessly, a workspace where a person runs coding agents
(Claude Code, Codex) in chats. The voice talks with the person; when it hands a task over, you decide what to do and
answer ONE JSON object and nothing else:

{"say": "<one or two short spoken sentences in the person's language, no markdown>",
 "mood": "neutral|joy|sad|thinking|surprised",
 "actions": [ ... ]}

Actions (use only these; ids and values only from the lists in the message):
- {"do":"agent","text":"..."}: real work for the CURRENT chat's agent (code, files, research, running things, anything
  that needs the machine). Write the text as a clear instruction to the agent in the person's language.
- {"do":"new_chat","text":"...","agent":"claude|codex","model":"<id>","effort":"<effort>","orchestrate":false}: start a
  new chat with that prompt. When there is no current chat, work goes here instead of "agent". For a big multi-step
  task (several parts, several repos, "сделай всё", a project) set "orchestrate":true, the strongest model and a high
  effort: that chat will split the work and run other chats itself. A simple task: a fast model, low effort.
- {"do":"model","model":"<agent:model id>","effort":"<effort>","chat":"<id>"}: switch a chat's model and/or effort
  («переключи на опус», «поставь глубже», «сделай быстрее»); without "chat" it is the CURRENT chat. Only a model or
  only an effort is fine. This is not a new chat.
- {"do":"send","chat":"<id>","text":"..."}: a message to ANOTHER chat the person names or clearly means.
- {"do":"open_chat","chat":"<id>"}: show a chat.
- {"do":"answer","chat":"<id>","option":2} or {"do":"answer","chat":"<id>","text":"..."}: answer a chat's pending
  question; option is 1-based in the listed order; text when none of the options fits what the person said.
- {"do":"permit","chat":"<id>","allow":true|false}: a chat's pending permission.
- {"do":"open","page":"<page id>","section":"<section id>"}: open a page (section only for settings).
- {"do":"theme","value":"<theme id>"}, {"do":"sidebar","open":true|false}, {"do":"pane","open":true|false,"tab":"<tab>"},
  {"do":"setting","key":"<key>","value":"<value>"}.
- {"do":"show","what":"chats|text|chat","text":"...","chat":"<id>"}: put a card on the voice's screen (a list of the
  chats, a text worth reading, a chat's summary) when the person asks to see something or a list is easier to read.
- {"do":"read_aloud","source":"chat|answer|text|file","chat":"<id>","text":"...","path":"..."}: read something long
  out loud in full (a document, an agent's answer); the voice reads it in parts and can be paused. When the screen
  says a reading is PAUSED and the person wants it to go on («продолжи», «дальше», «читай»), {"do":"read_aloud",
  "source":"resume"}.
- {"do":"recall","query":"..."}: the person refers to something from before («помнишь», «вчера обсуждали», «как мы
  решили»). Use it alone, with a short "say"; you will be asked again with what was found.
- {"do":"mood","value":"..."} is not needed: put the mood in "mood".
- {"do":"stop"}: the person ends the talk.

Rules:
- Prefer doing over asking. Pick the most likely chat from titles, states and the current one; a question or a
  permission without a named chat goes to the chat that is waiting for it (the current one first).
- A question about the chats (what is going on, who waits for me) is answered in "say" from the list, with no action,
  or with a "show" of the chats when there are many.
- When a chat is waiting on a question (ASKS) and the person speaks to that question — names it, its topic or its
  options, even in their own words — it is an "answer" to that chat (text when no option fits), never "agent".
- Several actions are fine (open a chat and answer in it). Never invent ids, pages, themes or settings.
- "say" is spoken: short, natural, no ids, no lists of more than three things. Say what you did, not what you will do,
  unless the work went to an agent (then: it is handed over).
- A new, separate piece of work that is not about the current chat's topic («создай новый дизайн», «сделай отдельный
  сайт», «новый проект») is a "new_chat", not "agent": the current chat keeps its own work.
- When the screen says a ChatGPT talk is current, "agent" is that talk writing (texts, answers, plans); anything that
  needs a machine — a board, a design, a file, code, running something — is a "new_chat" for an agent.
- The text for an agent ("agent", "send", "new_chat") is the person's request, whole and in their words, so the
  agent can do it without the talk. Never tell an agent to ask the person for a key, a token or a password: the
  machine has its own access.
- «Синопия», «Sinopia», «макет», «прототип», «дизайн экрана» is Sinopia Design (the harnessly design app): its
  "new_chat" text says to make it in Sinopia Design.
- Small talk, an opinion, a joke: just "say", no actions.`

// Prompt is the message for one task: the task, the screen, the chats, the catalog, and what was recalled.
func Prompt(req Request, recalled []string) string {
	var b strings.Builder
	b.WriteString("Task from the voice: " + one(req.Task) + "\n")
	if s := one(req.Said); s != "" && s != one(req.Task) {
		b.WriteString("The person said: " + s + "\n")
	}
	if len(req.History) > 0 {
		b.WriteString("\nThe talk so far (oldest first):\n")
		for _, h := range tail(req.History, 8) {
			b.WriteString("- " + clip(one(h), 300) + "\n")
		}
	}
	if s := strings.TrimSpace(req.Screen); s != "" {
		b.WriteString("\nOn the screen now:\n" + clip(s, 1500) + "\n")
	}
	b.WriteString("\nChats (most recent first):\n")
	if len(req.Chats) == 0 {
		b.WriteString("- none\n")
	}
	for i, c := range req.Chats {
		if i >= 40 {
			break
		}
		fmt.Fprintf(&b, "- id=%s «%s»", c.ID, clip(one(c.Title), 80))
		if c.Current {
			b.WriteString(" [CURRENT]")
		}
		if c.State != "" {
			b.WriteString(" state=" + c.State)
		}
		if c.Agent != "" {
			b.WriteString(" agent=" + c.Agent)
		}
		if c.Updated != "" {
			b.WriteString(" updated=" + c.Updated)
		}
		if c.Last != "" {
			b.WriteString(" last: " + clip(one(c.Last), 200))
		}
		if c.Ask != nil {
			b.WriteString("\n    ASKS: " + clip(one(c.Ask.Question), 300))
			for j, o := range c.Ask.Options {
				fmt.Fprintf(&b, "\n      %d. %s", j+1, clip(one(o), 120))
			}
		}
		if c.Permission != "" {
			b.WriteString("\n    WANTS PERMISSION: " + clip(one(c.Permission), 300))
		}
		b.WriteString("\n")
	}
	cat := req.Catalog
	b.WriteString("\nCatalog:\n")
	b.WriteString("- pages: " + choices(cat.Pages) + "\n")
	if len(cat.Sections) > 0 {
		b.WriteString("- settings sections: " + choices(cat.Sections) + "\n")
	}
	if len(cat.Themes) > 0 {
		b.WriteString("- themes: " + choices(cat.Themes) + "\n")
	}
	for _, s := range cat.Settings {
		fmt.Fprintf(&b, "- setting %s («%s»)", s.Key, s.Label)
		if len(s.Values) > 0 {
			b.WriteString(" values: " + strings.Join(s.Values, ", "))
		}
		if s.Now != "" {
			b.WriteString(" now: " + s.Now)
		}
		b.WriteString("\n")
	}
	if len(cat.Agents) > 0 {
		b.WriteString("- new chat agents/models (agent:model): " + choices(cat.Agents) + "\n")
	}
	if len(cat.Efforts) > 0 {
		b.WriteString("- efforts: " + strings.Join(cat.Efforts, ", ") + "\n")
	}
	if len(cat.PaneTabs) > 0 {
		b.WriteString("- pane tabs: " + strings.Join(cat.PaneTabs, ", ") + "\n")
	}
	if recalled != nil {
		b.WriteString("\nRecalled from the person's memory and past chats (answer with this now, no recall action):\n")
		for _, r := range recalled {
			b.WriteString("- " + clip(one(r), 400) + "\n")
		}
	}
	b.WriteString("\nAnswer with the JSON object only.")
	return b.String()
}

func choices(list []Choice) string {
	if len(list) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(list))
	for _, c := range list {
		if c.Label != "" && c.Label != c.ID {
			parts = append(parts, c.ID+" («"+c.Label+"»)")
		} else {
			parts = append(parts, c.ID)
		}
	}
	return strings.Join(parts, ", ")
}

func one(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func tail(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[len(list)-n:]
}
