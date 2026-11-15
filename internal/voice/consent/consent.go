// Package consent sorts an agent's tool calls by what they touch, for the voice mode: while the person
// talks to a chat by voice, a call that reaches outside — a message to people, a push, a merge or a rollout, a task
// made or changed, a publication, a deletion, a payment, a letter — waits for their «да» (by voice or with the
// button); reading, looking around, opening and the chat's own files go at once.
//
// The sort is by tool and its input, not by the person's words: the agent may reach outside on a request that did not
// sound like it. It errs towards asking: a tool it does not know that writes somewhere is outside.
package consent

import (
	"regexp"
	"strings"
)

// Class of a tool call.
type Class string

const (
	Read     Class = "read"     // reads, looks, opens: goes at once
	Local    Class = "local"    // changes the chat's own files, undoable: goes at once
	External Class = "external" // reaches people or systems outside: waits for «да»
)

// Verdict is a call's class and, for an outside one, what it does in the person's words.
type Verdict struct {
	Class Class
	What  string
}

// readTools are Claude Code's and Codex's tools that only look.
var readTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true, "LS": true, "WebFetch": true, "WebSearch": true, "TodoWrite": true,
	"NotebookRead": true, "ToolSearch": true, "Skill": true, "Task": true, "Agent": true, "AskUserQuestion": true,
	"ExitPlanMode": true, "EnterPlanMode": true, "ListMcpResourcesTool": true, "ReadMcpResourceTool": true,
	"BashOutput": true, "Monitor": true, "TaskStop": true, "KillShell": true,
}

// localTools change the chat's own files.
var localTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

// shellOut are shell commands that reach outside, each with what it does.
var shellOut = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`\bgit\s+(\S+\s+)*push\b`), "отправить код в общий репозиторий (git push)"},
	{regexp.MustCompile(`\b(glab|gh)\s+(mr|pr)\s+(merge|create|close|approve)\b`), "изменить merge request"},
	{regexp.MustCompile(`\b(glab|gh)\s+(release|issue|repo)\s+(create|delete|edit|close)\b`), "изменить репозиторий или задачу"},
	{regexp.MustCompile(`\b(kubectl|helm)\s+(\S+\s+)*(apply|delete|rollout|scale|upgrade|install|uninstall|patch|edit)\b`), "изменить кластер"},
	{regexp.MustCompile(`\b(npm|pnpm|yarn)\s+publish\b|\bcargo\s+publish\b|\btwine\s+upload\b|\bdocker\s+push\b`), "опубликовать пакет или образ"},
	{regexp.MustCompile(`\bcurl\b[^|;&]*(-X\s*(POST|PUT|PATCH|DELETE)|--request[\s=]+(POST|PUT|PATCH|DELETE)|\s-[a-zA-Z]*[dFT]|--data|--form|--json|--upload-file)`), "отправить запрос во внешний сервис"},
	{regexp.MustCompile(`\bwget\b[^|;&]*--(post|method|body)`), "отправить запрос во внешний сервис"},
	{regexp.MustCompile(`\b(glab|gh)\s+api\b[^|;&]*(-X\s*(POST|PUT|PATCH|DELETE)|--method[\s=]+(POST|PUT|PATCH|DELETE)|\s-[fF]\s|--field|--raw-field|--input)`), "изменить репозиторий или задачу"},
	{regexp.MustCompile(`\b(scp|sftp|nc|ncat|socat|telnet)\s|\brsync\b[^|;&]*\S+:`), "передать данные на другую машину"},
	{regexp.MustCompile(`\b(aws|gcloud|gsutil|az|yc|s3cmd|terraform|pulumi)\s`), "изменить облако"},
	{regexp.MustCompile(`\bbase64\s+(-d|--decode)\b[^;&]*\|\s*(ba|z)?sh\b|\beval\s+["'$]`), "выполнить скрытую команду"},
	{regexp.MustCompile(`\b(sendmail|mail|mutt)\s`), "отправить письмо"},
	{regexp.MustCompile(`\b(tg-send|alerter|slack-send|notify-send-team)\b`), "написать людям"},
	{regexp.MustCompile(`\bharnessly\s+(secret\s+ask|share|publish)\b|\bartifact\s+(publish|share|delete)\b`), "опубликовать или поделиться"},
	{regexp.MustCompile(`\btracker\s+(new|start|parent|close)\b|/api/issues`), "завести или изменить задачу"},
	{regexp.MustCompile(`\brm\s+(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+`), "удалить файлы"},
	{regexp.MustCompile(`\bssh\s`), "выполнить команду на другой машине"},
}

// mcpOut are words of an MCP tool's name that make it reach outside.
var mcpOut = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`(^|_)(send|post|reply|notify|message|email|mail|invite)(_|$)`), "написать людям"},
	{regexp.MustCompile(`(^|_)(merge|push|deploy|release|rollout|publish|share|unshare)(_|$)`), "опубликовать или выкатить"},
	{regexp.MustCompile(`(^|_)(delete|remove|destroy|drop|archive|revoke)(_|$)`), "удалить"},
	{regexp.MustCompile(`(^|_)(pay|charge|refund|transfer|payout|order|purchase|buy)(_|$)`), "провести оплату"},
	{regexp.MustCompile(`(^|_)(create|update|add|set|change|manage|assign|link|log|comment|upsert|write|edit|execute|run|batch)(_|$)`), "изменить данные во внешней системе"},
}

// mcpLook are MCP tools that only look or open, by the first word of their names; checked after mcpOut.
var mcpLook = regexp.MustCompile(`(^|_)(get|list|find|search|query|read|show|open|describe|count|snapshot|screenshot|wait|status|logs|tables|freshness|ping|navigate|scroll|hover|press|click|type|back|kit|preview)(_|$)`)

// mcpOwn are MCP tools of the machine itself that change only the person's own things.
// Exact names only: a word inside another server's tool name («send_conversation_reply») does not make it ours.
var mcpOwn = regexp.MustCompile(`^(delegate_task|deliver_file|generate_images|wait_images|send_to_conversation|read_conversation|wait_conversation|list_children|notice_(show|update|close|ask)|file_selection|file_show|preview_[a-z_]+)$`)

// Classify sorts one call: the tool's name as the agent's CLI names it and the command of a shell call ("" for any
// other tool).
func Classify(tool, command string) Verdict {
	switch {
	case readTools[tool]:
		return Verdict{Class: Read}
	case localTools[tool]:
		return Verdict{Class: Local}
	case tool == "Bash" || tool == "shell" || tool == "exec_command" || tool == "local_shell":
		return shell(command)
	case strings.HasPrefix(tool, "mcp__"):
		return mcp(tool)
	}
	// A tool of its own name we do not know: outside, to be safe.
	return Verdict{Class: External, What: "вызвать инструмент " + tool}
}

func shell(command string) Verdict {
	c := strings.TrimSpace(command)
	for _, s := range shellOut {
		if s.re.MatchString(c) {
			return Verdict{Class: External, What: s.what}
		}
	}
	return Verdict{Class: Local}
}

func mcp(tool string) Verdict {
	parts := strings.Split(tool, "__")
	name := strings.ToLower(parts[len(parts)-1])
	// The machine's own: another chat of this person, a file handed over, a picture drawn, a note on screen.
	if mcpOwn.MatchString(name) {
		return Verdict{Class: Local}
	}
	// What reaches outside wins over a looking word: «get_and_delete» asks.
	for _, m := range mcpOut {
		if m.re.MatchString(name) {
			return Verdict{Class: External, What: m.what}
		}
	}
	if mcpLook.MatchString(name) {
		return Verdict{Class: Read}
	}
	// A name that says nothing: outside, to be safe.
	return Verdict{Class: External, What: "вызвать инструмент " + name}
}
