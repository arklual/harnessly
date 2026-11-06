package agentbrowser

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func (i *Instance) startupError(cause error) error {
	if i.profile.Kind == "attached" {
		return errors.New(sanitizeDiagnostic(cause.Error()))
	}
	f, err := os.Open(filepath.Join(i.dir, "host.log"))
	if err != nil {
		return errors.New(sanitizeDiagnostic(cause.Error()))
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return errors.New(sanitizeDiagnostic(cause.Error()))
	}
	raw, err := io.ReadAll(io.NewSectionReader(f, max(0, st.Size()-8192), 8192))
	if err != nil {
		return errors.New(sanitizeDiagnostic(cause.Error()))
	}
	return errors.New(startupReason(string(raw), cause.Error()))
}

func startupReason(log, fallback string) string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	// Prefer recognizable descriptions; otherwise redact potentially private diagnostic values.
	reasons := []string{
		"Socket path too long", "No usable sandbox", "Running as root without --no-sandbox",
		"Missing X server or $DISPLAY", "Failed to create a ProcessSingleton",
		"error while loading shared libraries", "Permission denied", "No such file or directory",
	}
	for n := len(lines) - 1; n >= 0; n-- {
		for _, reason := range reasons {
			if strings.Contains(lines[n], reason) {
				return "Chrome startup: " + reason + " (see host.log)"
			}
		}
	}
	for n := len(lines) - 1; n >= 0; n-- {
		if strings.Contains(lines[n], "FATAL") {
			return "Chrome startup: " + sanitizeDiagnostic(lines[n])
		}
	}
	for n := len(lines) - 1; n >= 0; n-- {
		if line := strings.TrimSpace(lines[n]); line != "" {
			return "Chrome startup: " + sanitizeDiagnostic(line)
		}
	}
	return sanitizeDiagnostic(fallback)
}

var diagnosticURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s]+`)
var diagnosticPath = regexp.MustCompile(`(?:[A-Za-z]:)?[/\\][^\s,;]+`)
var diagnosticCredential = regexp.MustCompile(`(?i)(?:token|password|secret|authorization|api[_-]?key|bearer)[=: ]+[^\s,;]+`)
var diagnosticQuoted = regexp.MustCompile("\"[^\"]*\"|'[^']*'")
var diagnosticOpaque = regexp.MustCompile(`[A-Za-z0-9_+/-]{24,}={0,2}`)

func sanitizeDiagnostic(line string) string {
	line = diagnosticQuoted.ReplaceAllString(line, "[value]")
	line = diagnosticURL.ReplaceAllString(line, "[address]")
	line = diagnosticPath.ReplaceAllString(line, "[path]")
	line = diagnosticCredential.ReplaceAllString(line, "[credential]")
	line = diagnosticOpaque.ReplaceAllString(line, "[value]")
	line = strings.Join(strings.Fields(line), " ")
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}
