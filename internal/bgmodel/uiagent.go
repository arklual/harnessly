package bgmodel

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The agent of the interface: a person says in words what they want of the window («открой настройку
// плагинов», «выключи продолжение после лимита») and a quick model answers at once, without a chat or a terminal. It
// can do two things besides answering: take the person to a place of the window, and offer to change a setting (the
// page asks the person before it changes anything). Both come as the first line of the model's answer, so the page
// acts on it before the words are written.

// UIPlace is a place of the window the agent can take the person to: a section of Settings, a screen, a setting as
// its page names it. ID is the page's own address of it; Words are other words it is found by.
type UIPlace struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Words string `json:"words,omitempty"`
}

// UIOption is one value a setting takes.
type UIOption struct {
	Value string
	Label string
}

// UISetting is a setting of the catalog as the agent is shown it.
type UISetting struct {
	Key        string
	Section    string
	Title      string
	Kind       string
	Options    []UIOption
	Min, Max   *int
	Value      string
	ValueLabel string
}

// The kinds of a UIAction.
const (
	UIActGo  = "go"
	UIActSet = "set"
)

// UIAction is what the agent does besides answering: Place to go to, or Key to change to Value (Label in words).
type UIAction struct {
	Kind  string
	Place string
	Key   string
	Value string
	Label string
}

// mapLine is a line of the window's map as the page writes it (features/settings/agent-map): «- <name>: /<path>».
var mapLine = regexp.MustCompile(`^\s*-\s*(.+?):\s*(/(?:[^/\s]\S*)?)\s*$`)

// PlacesOfMap reads the window's map: each line «- <name>: /<path>» is a place whose id is its path; other lines (the
// map's heading) are left out.
func PlacesOfMap(text string) []UIPlace {
	var out []UIPlace
	for _, line := range strings.Split(text, "\n") {
		if m := mapLine.FindStringSubmatch(line); m != nil {
			out = append(out, UIPlace{ID: m[2], Title: strings.TrimSpace(m[1])})
		}
	}
	return out
}

// The bounds of one request.
const (
	UIPlacesMax   = 160
	UISettingsMax = 80
	UIQuestionMax = 400
)

// pickLang is the words of the interface's language: lang "en" or "ru", else the machine's.
func pickLang(lang, en, ru string) string {
	switch lang {
	case "en":
		return en
	case "ru":
		return ru
	}
	return locale.Pick(en, ru)
}

func uiAgentCall(question, page, lang string, places []UIPlace, settings []UISetting) Call {
	if len(places) > UIPlacesMax {
		places = places[:UIPlacesMax]
	}
	if len(settings) > UISettingsMax {
		settings = settings[:UISettingsMax]
	}
	system := pickLang(lang,
		"You are the assistant of the harnessly window. You see the places of the window and the settings below. Write the first line as one of: «GO <place id>» to take the person to a place; «SET <setting key> = <value>» to change a setting (the window asks the person first; use only a value the setting lists); «-» when there is nothing to do. Then, on the next line, answer in one short sentence, in English: what you opened, or where to find it; after SET say what you offer to change, never that it is changed (the person has not said yes yet). Never say you cannot open something that is listed. No markdown.",
		"Ты помощник окна harnessly. Ниже места окна и настройки. Первой строкой напиши одно из: «GO <id места>» — перевести человека туда; «SET <ключ настройки> = <значение>» — поменять настройку (окно сначала спросит человека; бери только значение из списка настройки); «-» — если делать нечего. Затем со следующей строки ответь одним коротким предложением по-русски: что открыл или где это найти; после SET скажи, что предлагаешь поменять, и не пиши, что уже поменял (человек ещё не согласился). Никогда не говори, что не можешь открыть место из списка. Без разметки.")
	var b strings.Builder
	b.WriteString(pickLang(lang, "Places (id — name; other words):\n", "Места (id — название; другие слова):\n"))
	for _, p := range places {
		b.WriteString(clipRunes(p.ID, 80) + " — " + clipRunes(p.Title, 80))
		if w := clipRunes(p.Words, 160); w != "" {
			b.WriteString("; " + w)
		}
		b.WriteString("\n")
	}
	if len(settings) > 0 {
		b.WriteString(pickLang(lang, "\nSettings (key — name [section]: now; values):\n", "\nНастройки (ключ — название [раздел]: сейчас; значения):\n"))
		for _, s := range settings {
			b.WriteString(clipRunes(s.Key, 80) + " — " + clipRunes(s.Title, 100) + " [" + clipRunes(s.Section, 40) + "]: " + clipRunes(s.ValueLabel, 40))
			if vals := settingValues(s); vals != "" {
				b.WriteString("; " + vals)
			}
			b.WriteString("\n")
		}
	}
	if page = clipRunes(page, 80); page != "" {
		b.WriteString(pickLang(lang, "\nThe person is at: ", "\nЧеловек сейчас на: ") + page + "\n")
	}
	b.WriteString(pickLang(lang, "\nRequest: ", "\nПросьба: ") + clipRunes(question, UIQuestionMax) + "\n")
	// Said last, after the request: a model answers in the request's language unless told so right before it writes.
	b.WriteString(pickLang(lang, "\nAnswer in English, whatever the language of the request.\n",
		"\nОтвечай по-русски, на каком бы языке ни была просьба.\n"))
	return Call{System: system, Prompt: b.String(), MaxTokens: 200, Timeout: 15 * time.Second, Quick: true}
}

func settingValues(s UISetting) string {
	switch {
	case len(s.Options) > 0:
		parts := make([]string, 0, len(s.Options))
		for _, o := range s.Options {
			parts = append(parts, o.Value+"="+o.Label)
		}
		return strings.Join(parts, ", ")
	case s.Kind == "bool":
		return "on, off"
	case s.Min != nil && s.Max != nil:
		return strconv.Itoa(*s.Min) + "…" + strconv.Itoa(*s.Max)
	}
	return ""
}

// settingTakes says whether the setting takes the value (its own or its label's words): the value itself, and its
// words.
func settingTakes(s UISetting, value string) (string, string, bool) {
	if len(s.Options) > 0 {
		k := slices.IndexFunc(s.Options, func(o UIOption) bool { return o.Value == value || strings.EqualFold(o.Label, value) })
		if k < 0 {
			return "", "", false
		}
		return s.Options[k].Value, s.Options[k].Label, true
	}
	switch s.Kind {
	case "bool":
		if value == "on" {
			return value, locale.Pick("On", "Вкл"), true
		}
		if value == "off" {
			return value, locale.Pick("Off", "Выкл"), true
		}
		return "", "", false
	case "number":
		n, err := strconv.Atoi(value)
		if err != nil || (s.Min != nil && n < *s.Min) || (s.Max != nil && n > *s.Max) {
			return "", "", false
		}
		return value, value, true
	}
	return "", "", false
}

// The marks of an action in the model's words. The model is asked for a line of its own, first, but it may put the
// mark after the words, in the same line, in quotes or bold: wherever it is, a mark is taken and never shown.
var (
	uiGo   = regexp.MustCompile("[*`\"«]*\\bGO\\s+[*`\"«]*([^\\s*`\"»]+)[*`\"»]*")
	uiSet  = regexp.MustCompile("[*`\"«]*\\bSET\\s+[*`\"«]*([A-Za-z0-9_.-]+)[*`\"»]*\\s*=\\s*(.+)$")
	uiDash = regexp.MustCompile(`^\s*-\s*$`)
)

// uiFilter takes the action out of the model's words as they come and passes the rest on. A line's words are passed
// at once up to where an action's mark could start («G», «S», «-», a quote); that tail waits for the line's end.
type uiFilter struct {
	places   []UIPlace
	settings []UISetting
	onAction func(UIAction)
	onDelta  func(string)
	act      *UIAction
	line     strings.Builder
	sent     int
	out      strings.Builder
	newline  bool
}

func (f *uiFilter) write(piece string) {
	for piece != "" {
		i := strings.IndexByte(piece, '\n')
		if i < 0 {
			f.line.WriteString(piece)
			s := f.line.String()
			if h := holdFrom(s, f.sent); h > f.sent {
				f.say(s[f.sent:h])
				f.sent = h
			}
			return
		}
		f.line.WriteString(piece[:i])
		f.endLine()
		piece = piece[i+1:]
	}
}

func (f *uiFilter) endLine() {
	s := f.line.String()
	rest := f.marks(s[f.sent:], f.sent == 0)
	f.say(rest)
	if f.out.Len() > 0 {
		f.newline = true
	}
	f.line.Reset()
	f.sent = 0
}

// say passes words on: no blank start, and a line break only between lines that said something.
func (f *uiFilter) say(words string) {
	if f.out.Len() == 0 {
		words = strings.TrimLeft(words, " \t\r")
	}
	if strings.TrimSpace(words) == "" && (f.out.Len() == 0 || f.newline) {
		return
	}
	if f.newline {
		f.out.WriteString("\n")
		f.onDelta("\n")
		f.newline = false
	}
	f.out.WriteString(words)
	f.onDelta(words)
}

// holdFrom is where the words of a line stop being surely words: the first place from `from` where a mark could
// start, with the spaces before it (taken out with the mark); len(s) when there is none.
func holdFrom(s string, from int) int {
	at := markFrom(s, from)
	for at > from && (s[at-1] == ' ' || s[at-1] == '\t') {
		at--
	}
	return at
}

func markFrom(s string, from int) int {
	for i := from; i < len(s); i++ {
		c := s[i]
		if c != 'G' && c != 'S' && c != '-' && c != '*' && c != '`' && c != '"' && !strings.HasPrefix(s[i:], "«") {
			continue
		}
		if i > 0 && !strings.ContainsAny(s[i-1:i], " \t*`\"") && !strings.HasSuffix(s[:i], "«") {
			continue
		}
		t := strings.TrimLeft(s[i:], "*`\"«")
		if c == '-' && strings.TrimSpace(s[:i]) == "" {
			return i
		}
		for _, w := range []string{"GO ", "SET "} {
			if strings.HasPrefix(w, t) || strings.HasPrefix(t, w) || strings.HasPrefix(t, strings.TrimSpace(w)+"\t") {
				return i
			}
		}
	}
	return len(s)
}

// marks takes the marks out of the rest of a line, acting on the first one the request allows.
func (f *uiFilter) marks(text string, whole bool) string {
	if whole && uiDash.MatchString(text) {
		return ""
	}
	if m := uiSet.FindStringSubmatchIndex(text); m != nil {
		key := text[m[2]:m[3]]
		value := strings.Trim(strings.TrimSpace(text[m[4]:m[5]]), "*`\"«».;,!")
		if k := slices.IndexFunc(f.settings, func(s UISetting) bool { return s.Key == key }); k >= 0 && f.act == nil {
			if v, label, ok := settingTakes(f.settings[k], value); ok && v != f.settings[k].Value {
				f.do(UIAction{Kind: UIActSet, Key: key, Value: v, Label: label})
			}
		}
		text = cut(text, m[0], m[1], whole)
	}
	for {
		m := uiGo.FindStringSubmatchIndex(text)
		if m == nil {
			break
		}
		place := strings.TrimRight(text[m[2]:m[3]], ".,;:!?)»")
		if k := slices.IndexFunc(f.places, func(p UIPlace) bool { return p.ID == place }); k >= 0 && f.act == nil {
			f.do(UIAction{Kind: UIActGo, Place: place, Label: f.places[k].Title})
		}
		text = cut(text, m[0], m[1], whole)
	}
	return text
}

func (f *uiFilter) do(a UIAction) {
	f.act = &a
	f.onAction(a)
}

// cut takes text[i:j] out, with the spaces before it; a line that was the mark alone (whole: nothing of it was said yet)
// leaves nothing, not its full stop.
func cut(text string, i, j int, whole bool) string {
	before, after := text[:i], text[j:]
	if strings.HasSuffix(before, " ") && (after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, ".")) {
		before = strings.TrimRight(before, " ")
	}
	if strings.TrimSpace(after) == "." || strings.TrimSpace(after) == "" {
		after = strings.TrimSpace(after)
		if whole && strings.TrimSpace(before) == "" {
			after = ""
		}
	}
	return before + after
}

// UIAgentStream answers a request to the window. onAction is called once, as soon as an action's mark is read, and
// only for an action the request allows; onDelta gets the words of the answer as they come, without the mark. lang is
// the interface's language ("en", "ru"; else the machine's): the answer is in it. used is false when no model can
// answer.
func (r *Runner) UIAgentStream(ctx context.Context, question, page, lang string, places []UIPlace, settings []UISetting,
	onAction func(UIAction), onDelta func(string)) (text string, act *UIAction, used bool, err error) {
	call := uiAgentCall(question, page, lang, places, settings)
	f := &uiFilter{places: places, settings: settings, onAction: onAction, onDelta: onDelta}
	_, used, err = r.RunStream(ctx, FeatureUIAgent, call, f.write)
	if !used || err != nil {
		return "", f.act, used, err
	}
	f.endLine()
	text = strings.TrimSpace(f.out.String())
	if text == "" && f.act == nil {
		return "", nil, true, &Error{Reason: ReasonEmpty}
	}
	return text, f.act, true, nil
}
