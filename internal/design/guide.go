package design

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

//go:embed texts
var textFiles embed.FS

// The rules an agent follows when it edits a design of the library: the agent reads the instructions file of the folder
// it works in, and every comment from the panel names it. Written by the runtime; a person's own edits are kept: only a
// file still carrying an older marker is replaced.

const (
	GuideFile    = "CLAUDE.md"
	guideVersion = 5
	// DesignArtifactType is the published artifact type of the design tool: its instructions are read, never published to.
	DesignArtifactType = "https://claude.ai/artifact/QKN21svewxgyPb6SYRqWnd"
)

// text is an embedded text with the deployment's words put in. A text has an English file (guide.md) and a Russian one
// next to it (guide.ru.md); a file without a Russian twin (a stylesheet) is the same in both languages.
func (l *Library) text(name string) string {
	file, key := name, name
	if locale.Russian() {
		ext := filepath.Ext(name)
		if ru := strings.TrimSuffix(name, ext) + ".ru" + ext; textFileExists(ru) {
			file, key = ru, ru
		}
	}
	if v, ok := l.texts.Load(key); ok {
		return v.(string)
	}
	b, err := textFiles.ReadFile("texts/" + file)
	if err != nil {
		panic(err)
	}
	t := l.cfg.Texts
	out := strings.NewReplacer("{{tag}}", t.Tag, "{{cli}}", t.CLI, "{{host}}", t.Host, "{{shared}}", t.SharedDir,
		"{{kit}}", t.KitGlobal, "{{pages}}", t.PagesGlobal, "{{args}}", t.ArgsGlobal, "{{app}}", t.AppName).Replace(string(b))
	l.texts.Store(key, out)
	return out
}

func textFileExists(name string) bool {
	_, err := fs.Stat(textFiles, "texts/"+name)
	return err == nil
}

func (l *Library) GuidePath() string { return filepath.Join(l.Root(), GuideFile) }

func (l *Library) Guide() string { return l.text("guide.md") }

// EnsureGuide writes the rules next to the artifacts when they are missing or older than this runtime's.
func (l *Library) EnsureGuide() error {
	file := l.GuidePath()
	if cur, err := os.ReadFile(file); err == nil {
		marker := regexp.MustCompile(`^<!-- ` + regexp.QuoteMeta(l.cfg.Texts.Tag) + `-library-guide v(\d+) -->`)
		m := marker.FindStringSubmatch(string(cur))
		if m == nil {
			return nil
		}
		if v, err := strconv.Atoi(m[1]); err != nil || v >= guideVersion {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(l.Guide()), 0o666)
}
