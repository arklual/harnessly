package design

import (
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The video's rules are read on demand: an agent that works on anything else does not carry them. They are
// embedded with the other texts and read by `design video guide [topic]`.

// VideoGuideTopics are the topics in the order a newcomer needs them.
var VideoGuideTopics = [...]string{"basics", "director", "motion", "engines", "check"}

// VideoGuide is a topic's text with the command's name put in; false when there is no such topic.
func VideoGuide(topic, cli string) (string, bool) {
	known := false
	for _, t := range VideoGuideTopics {
		known = known || t == topic
	}
	if !known {
		return "", false
	}
	file := "texts/video/" + topic + ".md"
	if locale.Russian() {
		file = "texts/video/" + topic + ".ru.md"
	}
	raw, err := textFiles.ReadFile(file)
	if err != nil {
		panic(err)
	}
	return strings.ReplaceAll(string(raw), "{{cli}}", cli), true
}
