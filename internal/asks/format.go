package asks

import (
	"html"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/notifybots"
)

// Buttons' data: "ha:<ask id>:<choice>" — the choices are "ok" and "fix" for an approval, "o<n>" for an option.
const dataPrefix = "ha:"

func esc(s string) string { return html.EscapeString(s) }

// mention tags the person: @username notifies a member of the chat; a person without one is tagged by id.
func mention(p Person) string {
	switch {
	case p.Username != "":
		return "@" + p.Username
	case p.TelegramID != 0:
		name := p.Name
		if name == "" {
			name = "?"
		}
		return `<a href="tg://user?id=` + strconv.FormatInt(p.TelegramID, 10) + `">` + esc(name) + `</a>`
	}
	return esc(p.Name)
}

// messageHTML is the ask as the chat sees it.
func messageHTML(a Ask, source string) string {
	var b strings.Builder
	if a.Kind == KindApproval {
		b.WriteString(locale.Pick("🟢 <b>Approval needed</b>", "🟢 <b>Нужно согласование</b>"))
	} else {
		b.WriteString(locale.Pick("❓ <b>A question from an agent</b>", "❓ <b>Вопрос от агента</b>"))
	}
	b.WriteString("\n\n")
	if a.To != nil {
		b.WriteString(mention(*a.To) + ", ")
	}
	b.WriteString(notifybots.ToHTML(a.Question))
	if a.Details != "" {
		b.WriteString("\n\n" + notifybots.ToHTML(a.Details))
	}
	if a.Link != "" {
		b.WriteString("\n\n🔗 " + esc(a.Link))
	}
	b.WriteString("\n\n<i>")
	if source != "" {
		b.WriteString(esc(source) + " · ")
	}
	switch {
	case a.Kind == KindApproval:
		b.WriteString(locale.Pick("press a button or reply to this message", "нажмите кнопку или ответьте на это сообщение"))
	case len(a.Options) > 0:
		b.WriteString(locale.Pick("pick an answer or reply to this message", "выберите ответ или ответьте на это сообщение"))
	default:
		b.WriteString(locale.Pick("reply to this message", "ответьте на это сообщение"))
	}
	b.WriteString("</i>")
	return b.String()
}

// buttons are the inline keyboard of an ask.
func buttons(a Ask) [][]notifybots.Button {
	var rows [][]notifybots.Button
	data := func(choice string) string { return dataPrefix + a.ID + ":" + choice }
	if a.Kind == KindApproval {
		rows = append(rows, []notifybots.Button{
			{Text: locale.Pick("✅ Approve", "✅ Одобрить"), Data: data("ok")},
			{Text: locale.Pick("✏️ Request changes", "✏️ Нужны правки"), Data: data("fix")},
		})
	}
	for i, o := range a.Options {
		rows = append(rows, []notifybots.Button{{Text: o, Data: data("o" + strconv.Itoa(i))}})
	}
	if a.Link != "" && (strings.HasPrefix(a.Link, "https://") || strings.HasPrefix(a.Link, "http://")) {
		rows = append(rows, []notifybots.Button{{Text: locale.Pick("Open", "Открыть"), URL: a.Link}})
	}
	return rows
}

// parseData reads a button's data: the ask and the choice.
func parseData(s string) (id, choice string, ok bool) {
	rest, ok := strings.CutPrefix(s, dataPrefix)
	if !ok {
		return "", "", false
	}
	id, choice, ok = strings.Cut(rest, ":")
	return id, choice, ok && id != "" && choice != ""
}

// outcomeHTML is the line the message is closed with.
func outcomeHTML(a Ask) string {
	who := ""
	if a.Answer != nil {
		who = " — " + esc(a.Answer.From.Label())
		if a.Answer.Via == "web" {
			who += locale.Pick(" (in harnessly)", " (в harnessly)")
		}
	}
	switch a.Status {
	case StatusAnswered:
		switch a.Decision {
		case DecisionApproved:
			return locale.Pick("✅ <b>Approved</b>", "✅ <b>Одобрено</b>") + who
		case DecisionChanges:
			return locale.Pick("✏️ <b>Changes requested</b>", "✏️ <b>Нужны правки</b>") + who
		}
		return locale.Pick("💬 <b>Answered</b>", "💬 <b>Ответ получен</b>") + who
	case StatusTimeout:
		return locale.Pick("⏱ <b>No longer waiting</b>: the agent went on without an answer", "⏱ <b>Ответ больше не ждут</b>: агент продолжил без него")
	case StatusCancelled:
		return locale.Pick("✖️ <b>Called off</b>", "✖️ <b>Вопрос снят</b>")
	}
	return ""
}

// closedHTML is the message with the outcome under it, without the hint to answer.
func closedHTML(a Ask) string {
	text := a.HTML
	if i := strings.LastIndex(text, "\n\n<i>"); i > 0 {
		text = text[:i]
	}
	return text + "\n\n" + outcomeHTML(a)
}
