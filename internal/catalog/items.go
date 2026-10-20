package catalog

import (
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

var sourceLabels = map[string]locale.Text{
	"quick":   locale.T("quick", "быстрая"),
	"builtin": locale.T("built-in", "встроенная"),
	"project": locale.T("project", "проект"),
	"user":    locale.T("mine", "моя"),
	"skill":   locale.T("skill", "навык"),
	"plugin":  locale.T("plugin", "плагин"),
}

const (
	maxItems          = 2000
	maxDescriptionLen = 240
)

var tooManyWarning = locale.T("Too many commands: only the first 2000 are shown", "Команд слишком много — показаны первые 2000")

// Item is one command a composer can offer. Field order is the answer's.
type Item struct {
	// ID is "<source>:<plugin>:<name>", unique within a catalog.
	ID string `json:"id"`
	// Name is without the trigger: "compact", "commit-commands:commit".
	Name         string   `json:"name"`
	Trigger      string   `json:"trigger"`
	Aliases      []string `json:"aliases"`
	Description  string   `json:"description"`
	ArgumentHint *string  `json:"argumentHint"`
	Source       string   `json:"source"`
	SourceLabel  string   `json:"sourceLabel"`
	Plugin       *string  `json:"plugin"`
	// Execution: send, model, effort, restart, help, skills, menu, confirm, terminal, blocked.
	Execution string `json:"execution"`
	// Output: transcript, reply, screen, none.
	Output string `json:"output"`
	// Welcome: allowed as the first message of a new conversation.
	Welcome bool `json:"welcome"`
	// Hidden: listed only under the whole list.
	Hidden bool `json:"hidden"`
	// Note: why a command is blocked, or the question a confirm command asks.
	Note   *string `json:"note"`
	Danger bool    `json:"danger"`
}

// Counts is the number of items per source.
type Counts struct {
	Quick   int `json:"quick"`
	Builtin int `json:"builtin"`
	Project int `json:"project"`
	User    int `json:"user"`
	Skill   int `json:"skill"`
	Plugin  int `json:"plugin"`
}

func (c *Counts) add(source string) {
	switch source {
	case "quick":
		c.Quick++
	case "builtin":
		c.Builtin++
	case "project":
		c.Project++
	case "user":
		c.User++
	case "skill":
		c.Skill++
	case "plugin":
		c.Plugin++
	}
}

// Catalog is the answer for one agent and folder.
type Catalog struct {
	Provider    string   `json:"provider"`
	Cwd         string   `json:"cwd"`
	GeneratedAt int64    `json:"generatedAt"`
	Items       []Item   `json:"items"`
	Counts      Counts   `json:"counts"`
	Warnings    []string `json:"warnings"`
	Truncated   bool     `json:"truncated"`
}

var commandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func strPtr(s string) *string { return &s }

// clipDescription: whitespace collapsed, at most 240 UTF-16 units with an ellipsis.
func clipDescription(text string) string {
	plain := trim(collapseSpace(text))
	if utf16Len(plain) > maxDescriptionLen {
		return utf16Prefix(plain, maxDescriptionLen-1) + "…"
	}
	return plain
}

// builtin is one row of a curated table of a CLI's own commands.
type builtin struct {
	name         string
	aliases      []string
	description  locale.Text
	argumentHint locale.Text
	execution    string
	output       string
	welcome      bool
	hidden       bool
	note         locale.Text
	danger       bool
}

func builtinItem(row builtin) Item {
	it := Item{
		ID:          "builtin::" + row.name,
		Name:        row.name,
		Trigger:     "/",
		Aliases:     append([]string{}, row.aliases...),
		Description: row.description.String(),
		Source:      "builtin",
		SourceLabel: sourceLabels["builtin"].String(),
		Execution:   row.execution,
		Output:      row.output,
		Welcome:     row.welcome,
		Hidden:      row.hidden,
		Danger:      row.danger,
	}
	if it.Output == "" {
		it.Output = "none"
	}
	if hint := row.argumentHint.String(); hint != "" {
		it.ArgumentHint = strPtr(hint)
	}
	if note := row.note.String(); note != "" {
		it.Note = strPtr(note)
	}
	return it
}

// promptItem: a command, skill or quick prompt, typed as is; its answer is the model's reply.
func promptItem(name, trigger, source string, plugin *string, description string, argumentHint *string) Item {
	pluginID, label := "", sourceLabels[source].String()
	if plugin != nil {
		pluginID = *plugin
		if *plugin != "" {
			label = sourceLabels["plugin"].String() + " · " + *plugin
		}
	}
	return Item{
		ID:           source + ":" + pluginID + ":" + name,
		Name:         name,
		Trigger:      trigger,
		Aliases:      []string{},
		Description:  clipDescription(description),
		ArgumentHint: argumentHint,
		Source:       source,
		SourceLabel:  label,
		Plugin:       plugin,
		Execution:    "send",
		Output:       "reply",
		Welcome:      true,
	}
}

// builder keeps the first item per trigger and name (sources are added in priority order) and the item limit.
type builder struct {
	items     []Item
	warnings  []string
	truncated bool
	names     map[string]bool
}

func newBuilder() *builder {
	return &builder{items: []Item{}, warnings: []string{}, names: map[string]bool{}}
}

func (b *builder) has(trigger, name string) bool { return b.names[trigger+strings.ToLower(name)] }

func (b *builder) add(it Item) {
	if !commandName.MatchString(it.Name) || b.has(it.Trigger, it.Name) {
		return
	}
	if len(b.items) >= maxItems {
		b.truncated = true
		return
	}
	b.names[it.Trigger+strings.ToLower(it.Name)] = true
	for _, a := range it.Aliases {
		b.names[it.Trigger+strings.ToLower(a)] = true
	}
	b.items = append(b.items, it)
}

func (b *builder) warn(msg string) {
	for _, w := range b.warnings {
		if w == msg {
			return
		}
	}
	b.warnings = append(b.warnings, msg)
}
