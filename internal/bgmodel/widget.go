package bgmodel

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// WidgetDefaultModel is the machine's Claude model a smart widget runs on when nothing is picked for FeatureWidget.
const WidgetDefaultModel = "haiku"

// The bounds of a smart widget's call: the question the widget's manifest asks, the data the page gives it
// (what the widget shows, never a secret or a chat), and a short answer.
const (
	WidgetPromptMax = 2000
	WidgetDataMax   = 8000
	WidgetAnswerMax = 400
)

const widgetSystem = "You write the text of a small widget on a dashboard. Answer the person's request in at most " +
	"three short lines, plain text, no markdown, no preamble, in the language of the request. Use only the data given; " +
	"say so in a few words when it is not enough."

// Widget answers a smart widget's request over its data: a few short lines. used is false when no model can answer.
func (r *Runner) Widget(ctx context.Context, prompt, data string) (text string, used bool, err error) {
	prompt = clip(strings.TrimSpace(prompt), WidgetPromptMax)
	data = clip(strings.TrimSpace(data), WidgetDataMax)
	full := prompt
	if data != "" {
		full += "\n\nData:\n" + data
	}
	raw, used, err := r.Run(ctx, FeatureWidget, Call{System: widgetSystem, Prompt: full, MaxTokens: 300, Timeout: 60 * time.Second, Quick: true})
	if !used || err != nil {
		return "", used, err
	}
	return clip(strings.TrimSpace(raw), WidgetAnswerMax), true, nil
}

// clip cuts s to at most n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
