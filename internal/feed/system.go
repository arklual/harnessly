package feed

import (
	"regexp"
	"strings"
)

// What the harness types into a session that is not the person's words and that the transcript layer leaves as a user
// line: a <task-notification> of any shape (a goal check-in has only a <summary>), <system-reminder> blocks alone, a
// line under «[SYSTEM NOTIFICATION - NOT USER INPUT]». The feed shows it as a quiet system note, never as a message.
const (
	systemNotePrefix = "[SYSTEM NOTIFICATION - NOT USER INPUT]"
	// systemNoteCode is the code of the notice entry such a note is.
	systemNoteCode = "system-note"
)

var (
	reminderRE   = regexp.MustCompile(`<system-reminder>([\s\S]*?)</system-reminder>`)
	taskBlockRE  = regexp.MustCompile(`^<task-notification>([\s\S]*?)</task-notification>`)
	openTagRE    = regexp.MustCompile(`<([a-z][a-z0-9_-]*)>`)
	blankLinesRE = regexp.MustCompile(`\n{3,}`)
)

// systemNote is the note a user line is when the harness wrote it: its first line is what the note says, the rest
// its details. ok false for anything else.
func systemNote(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	var reminders []string
	for _, m := range reminderRE.FindAllStringSubmatch(trimmed, -1) {
		r := strings.TrimSpace(m[1])
		if inner, ok := systemNote(r); ok {
			// A notification wrapped in a reminder reads as the notification.
			r = inner
		}
		if r != "" {
			reminders = append(reminders, r)
		}
	}
	rest := strings.TrimSpace(reminderRE.ReplaceAllString(trimmed, ""))
	switch {
	case strings.HasPrefix(rest, systemNotePrefix):
		body := strings.TrimSpace(strings.TrimPrefix(rest, systemNotePrefix))
		return joinNote(append([]string{body}, reminders...)), true
	case taskBlockRE.MatchString(rest):
		m := taskBlockRE.FindStringSubmatchIndex(rest)
		summary, details := taskNote(rest[m[2]:m[3]])
		after := strings.TrimSpace(rest[m[1]:])
		parts := append([]string{summary}, details...)
		if after != "" {
			parts = append(parts, after)
		}
		return joinNote(append(parts, reminders...)), true
	case rest == "" && len(reminders) > 0:
		return joinNote(reminders), true
	}
	return "", false
}

// taskNote is what a task notification says: its summary (else its first words) and its other fields, one a line.
func taskNote(inner string) (string, []string) {
	summary := ""
	var details []string
	loose := inner
	for at := 0; at < len(inner); {
		loc := openTagRE.FindStringSubmatchIndex(inner[at:])
		if loc == nil {
			break
		}
		tag := inner[at+loc[2] : at+loc[3]]
		start := at + loc[1]
		closing := "</" + tag + ">"
		end := strings.Index(inner[start:], closing)
		if end < 0 {
			at = start
			continue
		}
		value := strings.TrimSpace(inner[start : start+end])
		loose = strings.Replace(loose, inner[at+loc[0]:start+end+len(closing)], "", 1)
		at = start + end + len(closing)
		switch {
		case value == "":
		case tag == "summary" && summary == "":
			summary = value
		default:
			details = append(details, tag+": "+value)
		}
	}
	if loose = strings.TrimSpace(loose); loose != "" {
		details = append(details, loose)
	}
	if summary == "" && len(details) > 0 {
		summary, details = details[0], details[1:]
	}
	return summary, details
}

func joinNote(parts []string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return blankLinesRE.ReplaceAllString(strings.Join(kept, "\n"), "\n\n")
}
