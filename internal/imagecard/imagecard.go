// Package imagecard reads what the feed's picture card needs out of the text an agent's picture tool left in a
// transcript: the report of generate_images / wait_images (see imagegen.Report) and the names of the tools.
package imagecard

import (
	"regexp"
	"strings"
)

// States of a picture call.
const (
	Drawing = "drawing"
	Done    = "done"
	Failed  = "failed"
)

// IsGenerate and IsWait say whether a tool name (bare, or mcp__<server>__<tool>) is the harnessly picture tool.
func IsGenerate(tool string) bool {
	return tool == "generate_images" || strings.HasSuffix(tool, "__generate_images")
}
func IsWait(tool string) bool {
	return tool == "wait_images" || strings.HasSuffix(tool, "__wait_images")
}

// Report is the parsed answer of a picture tool.
type Report struct {
	JobID string
	State string
	Paths []string
	Error string
}

var pathLineRE = regexp.MustCompile(`^\d+\.\s+(/\S.*?)\s*$`)

// ParseReport reads the text of a generate_images / wait_images answer: «job_id: <id>», then «Done: …», «Still drawing: …»
// or «Failed: <why>», then «<n>. <path>» lines. ok is false for any other text.
func ParseReport(text string) (r Report, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return r, false
	}
	id, found := strings.CutPrefix(strings.TrimSpace(lines[0]), "job_id:")
	if !found {
		return r, false
	}
	r.JobID = strings.TrimSpace(id)
	r.State = Done
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Still drawing"):
			r.State = Drawing
		case strings.HasPrefix(line, "Failed:"):
			r.State = Failed
			r.Error = strings.TrimSpace(strings.TrimPrefix(line, "Failed:"))
		default:
			if m := pathLineRE.FindStringSubmatch(line); m != nil {
				r.Paths = append(r.Paths, m[1])
			}
		}
	}
	return r, r.JobID != ""
}
