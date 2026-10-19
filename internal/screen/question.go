package screen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Claude Code's AskUserQuestion dialog: one tab per question, a review screen at the end whose option "1" is "Submit
// answers". The keys are typed blind, so the answer counts as taken only once the dialog has left the screen.

const (
	// MaxAnswerOptions is the most options a question may have.
	MaxAnswerOptions = 8
	// KeyStepDelay follows every key or text; KeyAdvanceDelay follows each question's keys.
	KeyStepDelay    = 150 * time.Millisecond
	KeyAdvanceDelay = 350 * time.Millisecond
	// AnswerConfirmPoll and AnswerConfirmTimeout pace the wait for the dialog to leave the screen.
	AnswerConfirmPoll    = 250 * time.Millisecond
	AnswerConfirmTimeout = 3000 * time.Millisecond
)

// QuestionShape is the part of a question the keys depend on.
type QuestionShape struct {
	OptionCount int  `json:"optionCount"`
	MultiSelect bool `json:"multiSelect"`
	// Preview: the options carry previews.
	Preview bool `json:"preview"`
}

// QuestionChoice is what the person chose for one question: option indexes, and the free text of the "Other" row (""
// when not used).
type QuestionChoice struct {
	Picked []int  `json:"picked"`
	Other  string `json:"other"`
}

// KeyStep is one thing to send: a tmux key name, or text typed literally.
type KeyStep struct {
	Literal bool
	Value   string
}

func keyStep(name string) KeyStep { return KeyStep{Value: name} }

func digitStep(index int) KeyStep { return keyStep(strconv.Itoa(index + 1)) }

// previewLayout: a single-choice question with previews is drawn side by side with the preview. A digit there only
// moves the cursor, Enter picks, and notes replace the "Other" row.
func previewLayout(s QuestionShape) bool { return s.Preview && !s.MultiSelect }

// TakesFreeText: the question has an "Other" row — every layout but the single-choice one with previews.
func TakesFreeText(s QuestionShape) bool { return !previewLayout(s) }

// IsInstantAnswer: one single-choice question answers on the key press itself; every other call ends with the review
// screen.
func IsInstantAnswer(shapes []QuestionShape) bool { return len(shapes) == 1 && !shapes[0].MultiSelect }

// IsChoiceComplete: a pick, or text once "Other" is open.
func IsChoiceComplete(c QuestionChoice, otherOn bool) bool {
	if otherOn {
		return trim(c.Other) != ""
	}
	return len(c.Picked) > 0
}

func questionSteps(s QuestionShape, c QuestionChoice) []KeyStep {
	other := trim(c.Other)
	picked := slices.Clone(c.Picked)
	slices.Sort(picked)

	if previewLayout(s) {
		return []KeyStep{digitStep(picked[0]), keyStep("Enter")}
	}
	if !s.MultiSelect {
		if other != "" {
			return []KeyStep{digitStep(s.OptionCount), {Literal: true, Value: other}, keyStep("Enter")}
		}
		return []KeyStep{digitStep(picked[0])}
	}
	// A digit toggles an option and leaves the cursor on the first row; the "Other" row takes text only under the
	// cursor.
	steps := make([]KeyStep, 0, len(picked)+s.OptionCount+3)
	for _, p := range picked {
		steps = append(steps, digitStep(p))
	}
	if other == "" {
		return append(steps, keyStep("Right"))
	}
	for range s.OptionCount {
		steps = append(steps, keyStep("Down"))
	}
	return append(steps, KeyStep{Literal: true, Value: other}, keyStep("Down"), keyStep("Enter"))
}

// BuildAnswerSteps is the keys for every question in turn, one group per question, then "1" for "Submit answers"
// unless the call is one single-choice question. The request must have passed ValidateAnswerRequest.
func BuildAnswerSteps(shapes []QuestionShape, choices []QuestionChoice) [][]KeyStep {
	groups := make([][]KeyStep, 0, len(shapes)+1)
	for i, s := range shapes {
		groups = append(groups, questionSteps(s, choices[i]))
	}
	if IsInstantAnswer(shapes) {
		return groups
	}
	return append(groups, []KeyStep{keyStep("1")})
}

// ValidateAnswerRequest refuses what the dialog could not take; nil when the choices fit the questions.
func ValidateAnswerRequest(shapes []QuestionShape, choices []QuestionChoice) error {
	if len(shapes) == 0 || len(shapes) != len(choices) {
		return fmt.Errorf("questions and choices must be the same non-empty length")
	}
	for i, s := range shapes {
		n := i + 1
		picked, other := choices[i].Picked, trim(choices[i].Other)
		if !TakesFreeText(s) && other != "" {
			return fmt.Errorf("question %d: no free text next to previews", n)
		}
		if s.OptionCount < 1 || s.OptionCount > MaxAnswerOptions {
			return fmt.Errorf("question %d: bad option count", n)
		}
		for _, p := range picked {
			if p < 0 || p >= s.OptionCount {
				return fmt.Errorf("question %d: option out of range", n)
			}
		}
		sorted := slices.Clone(picked)
		slices.Sort(sorted)
		if len(slices.Compact(sorted)) != len(picked) {
			return fmt.Errorf("question %d: repeated option", n)
		}
		answers := len(picked)
		if other != "" {
			answers++
		}
		if !s.MultiSelect && answers != 1 {
			return fmt.Errorf("question %d: exactly one answer expected", n)
		}
		if s.MultiSelect && len(picked) == 0 && other == "" {
			return fmt.Errorf("question %d: no answer", n)
		}
	}
	return nil
}

// KeyIO sends answer steps to the pane.
type KeyIO interface {
	Key(name string) error
	Text(text string) error
	Sleep(d time.Duration)
}

// PlayAnswerSteps sends the groups in order, KeyStepDelay after every step and KeyAdvanceDelay after every group. It
// stops at the first error.
func PlayAnswerSteps(groups [][]KeyStep, io KeyIO) error {
	for _, group := range groups {
		for _, step := range group {
			var err error
			if step.Literal {
				err = io.Text(step.Value)
			} else {
				err = io.Key(step.Value)
			}
			if err != nil {
				return err
			}
			io.Sleep(KeyStepDelay)
		}
		io.Sleep(KeyAdvanceDelay)
	}
	return nil
}

const (
	questionFooter      = "Enter to select"
	reviewPrompt        = "Ready to submit your answers?"
	questionTailLines   = 5
	previewLayoutFooter = "n to add notes"
)

// IsQuestionOnScreen: the bottom of a plain Claude screen is the AskUserQuestion dialog — one of its question tabs
// (the footer starts with "Enter to select" and may wrap) or the review screen. Only the last 5 non-empty lines count:
// the conversation above may quote the words.
func IsQuestionOnScreen(screen string) bool {
	for _, line := range tail(trimmedNonEmpty(screen), questionTailLines) {
		if strings.HasPrefix(line, questionFooter) || line == reviewPrompt {
			return true
		}
	}
	return false
}

// PreviewFromScreen stands in for a question's missing preview flag (a page of an older release sends none): a lone
// question has the preview layout when its footer offers notes; several questions are tabs whose layouts differ, so
// they keep the plain keys.
func PreviewFromScreen(questionCount int, screen string) bool {
	return questionCount == 1 && strings.Contains(screen, previewLayoutFooter)
}

// WaitForQuestionGone polls the screen every AnswerConfirmPoll for up to AnswerConfirmTimeout until the dialog is gone.
// read returns false when the screen cannot be read.
func WaitForQuestionGone(read func() (string, bool), sleep func(time.Duration)) bool {
	for waited := time.Duration(0); waited < AnswerConfirmTimeout; waited += AnswerConfirmPoll {
		sleep(AnswerConfirmPoll)
		if s, ok := read(); ok && !IsQuestionOnScreen(s) {
			return true
		}
	}
	return false
}

const answerSeparator = ", "

// ParseAnswer reads back the answer Claude Code records for a question: the labels of the picked options joined by
// ", " in click order, or the free text of "Other". Labels may themselves hold ", ", so the text is consumed label by
// label, and what no label matches is the free text.
func ParseAnswer(answer string, labels []string) QuestionChoice {
	picked := []int{}
	rest := answer
	for rest != "" {
		index := -1
		for i, label := range labels {
			if !slices.Contains(picked, i) && (rest == label || strings.HasPrefix(rest, label+answerSeparator)) {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		picked = append(picked, index)
		rest = dropBytes(rest, len(labels[index])+len(answerSeparator))
	}
	return QuestionChoice{Picked: picked, Other: rest}
}

// dropBytes drops the first n bytes, or everything when the string is shorter.
func dropBytes(s string, n int) string {
	if n >= len(s) {
		return ""
	}
	return s[n:]
}
