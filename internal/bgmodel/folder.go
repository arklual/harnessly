package bgmodel

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The bounds of a folder suggestion: the person's last prompts of the chat, the folders with a few of the
// chats each holds (their titles say what the folder is for), and a one-word answer.
const FolderPromptMax = 600

// FolderPrompts is how many of the chat's last prompts the model reads.
const FolderPrompts = 6

// FolderMax is how many folders it is told of.
const FolderMax = 40

// FolderTitlesMax is how many chat titles of one folder it is told.
const FolderTitlesMax = 5

// FolderTitleMax and FolderNameMax cut a title and a name.
const (
	FolderTitleMax = 80
	FolderNameMax  = 80
)

const folderAnswerMax = 200
const folderNoneAnswer = "none"

// FolderInfo is one folder of the person: its id (what the answer names), its name and some of its chats' titles.
type FolderInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Titles []string `json:"titles"`
}

const folderSystem = "You sort chats of a person into their folders. You are given the person's messages of one chat " +
	"and the folders, each with its name and the titles of chats already in it. Pick the one folder the chat clearly " +
	"belongs to by its topic. When no folder clearly fits, or you are unsure, answer none. Answer with the folder's id " +
	"alone, or the word none: no other text."

// SuggestFolder picks a folder for a chat from its prompts: the folder's id, or "" when none fits. used is false when no
// model can answer.
func (r *Runner) SuggestFolder(ctx context.Context, prompts []string, folders []FolderInfo) (id string, used bool, err error) {
	if len(prompts) > FolderPrompts {
		prompts = prompts[len(prompts)-FolderPrompts:]
	}
	if len(folders) > FolderMax {
		folders = folders[:FolderMax]
	}
	if len(folders) == 0 || len(prompts) == 0 {
		return "", false, nil
	}
	var b strings.Builder
	b.WriteString("Folders:\n")
	for _, f := range folders {
		fmt.Fprintf(&b, "- id: %s | name: %s", f.ID, clip(f.Name, FolderNameMax))
		titles := f.Titles
		if len(titles) > FolderTitlesMax {
			titles = titles[:FolderTitlesMax]
		}
		for i, title := range titles {
			if i == 0 {
				b.WriteString(" | chats: ")
			} else {
				b.WriteString("; ")
			}
			b.WriteString(clip(strings.TrimSpace(title), FolderTitleMax))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nThe person's messages in the chat:\n")
	for i, p := range prompts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, clip(strings.TrimSpace(p), FolderPromptMax))
	}
	raw, used, err := r.Run(ctx, FeatureFolder, Call{System: folderSystem, Prompt: b.String(), MaxTokens: 40, Timeout: 45 * time.Second, Quick: true})
	if !used || err != nil {
		return "", used, err
	}
	return FolderFromAnswer(raw, folders), true, nil
}

// FolderFromAnswer reads the model's answer: the id of a folder it names, "" for none or for a word that is no folder
// of the list (a made-up id never reaches the page).
func FolderFromAnswer(answer string, folders []FolderInfo) string {
	answer = strings.Trim(strings.TrimSpace(clip(answer, folderAnswerMax)), "`\"'.* \n")
	if answer == "" || strings.EqualFold(answer, folderNoneAnswer) {
		return ""
	}
	for _, f := range folders {
		if f.ID == answer {
			return f.ID
		}
	}
	// A line with the id inside ("id: abc"), or the id after a label.
	best := ""
	for _, f := range folders {
		if strings.Contains(answer, f.ID) && len(f.ID) > len(best) {
			best = f.ID
		}
	}
	return best
}
