// Package sessionmeta reads what a Claude Code transcript says about the session itself, beside its messages: the
// reasoning effort it really runs on, the /goal it works toward, and the recap Claude writes for the person who comes
// back after being away. Claude writes them between the lines of the conversation: every model request is logged with its output_config (the effort the request went out with), the
// commands /effort and /goal answer in lines of their own, and the goal check leaves a goal_status attachment.
//
// A transcript is read once from its tail and then only what it gained, as internal/background does: a busy agent's
// every write costs its new lines.
package sessionmeta

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// tailBytes is how much of a transcript a cold read takes. A goal is restated at every check, so the tail has it; the
// ultracode mode is not, and is looked for before the tail when the tail does not say it. A var for the tests.
var tailBytes int64 = 16 << 20

// Goal statuses.
const (
	GoalActive = "active"
	GoalPaused = "paused"
	GoalMet    = "met"
)

// Goal is the /goal of a session. Since is when the goal was set, epoch milliseconds.
type Goal struct {
	Text   string
	Status string
	Since  int64
}

// Recap is Claude's «while you were away» summary: the last one the transcript has. At is when Claude wrote it,
// epoch milliseconds.
type Recap struct {
	Text string
	At   int64
}

// Tri is a fact a transcript may not state at all.
type Tri int

const (
	Unknown Tri = iota
	On
	Off
)

// Info is what a transcript says now.
type Info struct {
	// Effort is the level the last model request of the main thread went out with, or the one a later /effort set;
	// "" when the transcript does not say (an agent that has no effort, a version that did not log requests).
	Effort string
	// Ultracode is the mode as the running process last said it, the last word winning: /effort ultracode on|off, the
	// /effort dialog's toggle, and the reminders Claude Code itself attaches when the mode takes effect or ends
	// (ultra_effort_enter, ultra_effort_exit). A process that started since (a SessionStart hook of startup or resume)
	// has said nothing yet: Unknown, and what it runs on is its launch's. The whole transcript counts, not only the
	// tail a cold read takes. Since Claude Code 2.1.310 it is a mode beside the effort: a level set leaves it as it is.
	Ultracode Tri
	Goal      *Goal
	// Recap is the latest away_summary of the transcript; nil when it has none.
	Recap *Recap
	// Title is the session's own name: the title the person gave it in the agent (/rename, a custom-title line), else
	// the one Claude wrote after the first answer (the last ai-title line); "" when the transcript has neither yet.
	Title string
}

var (
	setEffortRE  = regexp.MustCompile(`^Set effort level to (\w+)`)
	keptEffortRE = regexp.MustCompile(`^Kept effort level as (\w+)`)
	ultraOnRE    = regexp.MustCompile(`^Ultracode on \(.*?Effort stays (\w+)`)
	ultraOffRE   = regexp.MustCompile(`^Ultracode off\. Effort stays (\w+)`)
	// The /effort dialog adds what its Tab toggle did to the line of the level it set: «… · Ultracode off».
	ultraToggleRE = regexp.MustCompile(`· Ultracode (on|off)\b`)
	goalSetRE     = regexp.MustCompile(`^Goal set: ([\s\S]+)$`)
	recapTail     = regexp.MustCompile(`\s*\(disable recaps in /config\)\s*$`)
	stdoutOpen    = "<local-command-stdout>"
	stdoutClose   = "</local-command-stdout>"
	efforts       = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	markers       = [][]byte{[]byte(`{"type":"api-request",`), []byte(`local-command-stdout>`), []byte(`goal_status`), []byte(`"Goal `), []byte(`Goal set:`), []byte(`away_summary`), []byte(`"ai-title"`), []byte(`"custom-title"`), []byte(`"ultra_effort_`), []byte(`"SessionStart:`)}
	// ultraNeedles find, before the tail, the lines that may say the mode; each found is read as a line to be sure.
	ultraNeedles = [][]byte{[]byte(`"ultra_effort_enter"`), []byte(`"ultra_effort_exit"`), []byte(`<local-command-stdout>Ultracode o`), []byte(`Ultracode on</local-command-stdout>`), []byte(`Ultracode off</local-command-stdout>`), []byte(`"SessionStart:startup"`), []byte(`"SessionStart:resume"`)}
)

// scan is what the transcript has said so far.
type scan struct {
	info Info
	// aiTitle and customTitle are the two names a transcript may carry; the person's own beats Claude's.
	aiTitle, customTitle string
	// ultraSaid: a line read said the mode, or started a process that has not said it yet.
	ultraSaid bool
}

type line struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	Operation   string `json:"operation"`
	Timestamp   string `json:"timestamp"`
	QuerySource string `json:"querySource"`
	Content     any    `json:"content"`
	AITitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	Message     struct {
		Content any `json:"content"`
	} `json:"message"`
	Params struct {
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	} `json:"params"`
	Attachment struct {
		Type      string `json:"type"`
		Met       bool   `json:"met"`
		Condition string `json:"condition"`
		HookName  string `json:"hookName"`
	} `json:"attachment"`
}

func interesting(raw []byte) bool {
	for _, m := range markers {
		if bytes.Contains(raw, m) {
			return true
		}
	}
	return false
}

func millis(ts string) int64 {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// feed takes the lines of a chunk in order.
func (s *scan) feed(raw []byte) {
	if !interesting(raw) {
		return
	}
	var l line
	if json.Unmarshal(raw, &l) != nil {
		return
	}
	at := millis(l.Timestamp)
	switch {
	case l.Type == "ai-title":
		if t := strings.TrimSpace(l.AITitle); t != "" {
			s.aiTitle = t
			s.setTitle()
		}
	case l.Type == "custom-title":
		if t := strings.TrimSpace(l.CustomTitle); t != "" {
			s.customTitle = t
			s.setTitle()
		}
	case l.Type == "api-request":
		if l.QuerySource == "repl_main_thread" {
			s.info.Effort = l.Params.OutputConfig.Effort
		}
	case l.Type == "attachment" && l.Attachment.Type == "goal_status":
		s.goalStatus(l.Attachment.Condition, l.Attachment.Met, at)
	case l.Type == "attachment" && l.Attachment.Type == "ultra_effort_enter":
		s.ultra(On)
	case l.Type == "attachment" && l.Attachment.Type == "ultra_effort_exit":
		s.ultra(Off)
	case l.Type == "attachment" && (l.Attachment.HookName == "SessionStart:startup" || l.Attachment.HookName == "SessionStart:resume"):
		// A new process: Claude Code keeps the mode for one process only, so what the one before said is not its.
		s.ultra(Unknown)
	case l.Type == "queue-operation" && l.Operation == "enqueue":
		if text, ok := l.Content.(string); ok {
			if m := goalSetRE.FindStringSubmatch(text); m != nil {
				s.goalText(strings.TrimSpace(m[1]), at)
			}
		}
	case l.Type == "system" && l.Subtype == "away_summary":
		if text, ok := l.Content.(string); ok {
			s.recap(text, at)
		}
	case l.Type == "system":
		if text, ok := l.Content.(string); ok {
			s.output(text, at)
		}
	case l.Type == "user":
		if text, ok := l.Message.Content.(string); ok && strings.HasPrefix(text, stdoutOpen) {
			s.output(text, at)
		}
	}
}

func (s *scan) setTitle() {
	s.info.Title = s.aiTitle
	if s.customTitle != "" {
		s.info.Title = s.customTitle
	}
}

// recap is an away_summary line. Claude ends its text with a hint to turn recaps off; that is not the recap's.
func (s *scan) recap(text string, at int64) {
	if text = strings.TrimSpace(recapTail.ReplaceAllString(text, "")); text == "" {
		return
	}
	s.info.Recap = &Recap{Text: text, At: at}
}

// toggle reads what the /effort dialog's Tab did to ultracode, said after the level it set.
func (s *scan) toggle(text string) {
	m := ultraToggleRE.FindStringSubmatch(text)
	switch {
	case m == nil:
	case m[1] == "on":
		s.ultra(On)
	default:
		s.ultra(Off)
	}
}

// ultra is a word of the transcript on the mode: the last one stands.
func (s *scan) ultra(t Tri) {
	s.info.Ultracode, s.ultraSaid = t, true
}

// output is a line of the agent's own: a command's output, wrapped in its tag, or a notice.
func (s *scan) output(text string, at int64) {
	if strings.HasPrefix(text, stdoutOpen) {
		s.command(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, stdoutOpen), stdoutClose)), at)
		return
	}
	s.notice(text, at)
}

// command is the output of a slash command the person ran.
func (s *scan) command(text string, at int64) {
	switch {
	case setEffortRE.MatchString(text):
		s.level(setEffortRE.FindStringSubmatch(text)[1])
		s.toggle(text)
	case keptEffortRE.MatchString(text):
		s.level(keptEffortRE.FindStringSubmatch(text)[1])
	case strings.HasPrefix(text, "Effort level set to auto"):
		s.toggle(text)
	case strings.HasPrefix(text, "Ultracode on"):
		s.ultra(On)
		if m := ultraOnRE.FindStringSubmatch(text); m != nil {
			s.level(m[1])
		}
	case ultraOffRE.MatchString(text):
		s.ultra(Off)
		s.level(ultraOffRE.FindStringSubmatch(text)[1])
	default:
		s.notice(text, at)
	}
}

func (s *scan) level(e string) {
	if efforts[e] {
		s.info.Effort = e
	}
}

// notice is a line the agent shows about the goal.
func (s *scan) notice(text string, at int64) {
	switch {
	case strings.HasPrefix(text, "Goal paused"):
		s.goalIs(GoalPaused)
	case strings.HasPrefix(text, "Goal still active"):
		s.goalIs(GoalActive)
	case strings.HasPrefix(text, "Goal achieved"):
		s.goalIs(GoalMet)
	case strings.HasPrefix(text, "Goal cleared"):
		s.info.Goal = nil
	default:
		if m := goalSetRE.FindStringSubmatch(text); m != nil {
			s.goalText(strings.TrimSpace(m[1]), at)
		}
	}
}

func (s *scan) goalIs(status string) {
	if s.info.Goal != nil {
		s.info.Goal.Status = status
	}
}

// goalText is a goal set: the same text again is the same goal.
func (s *scan) goalText(text string, at int64) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	if g := s.info.Goal; g != nil && g.Text == text {
		g.Status = GoalActive
		return
	}
	s.info.Goal = &Goal{Text: text, Status: GoalActive, Since: at}
}

// goalStatus is the goal check's attachment: written when the goal is taken up (the sentinel) and at every check.
func (s *scan) goalStatus(text string, met bool, at int64) {
	if text == "" {
		return
	}
	s.goalText(text, at)
	if met {
		s.goalIs(GoalMet)
	}
}

type cursor struct {
	ino     uint64
	offset  int64
	mtime   time.Time
	size    int64
	scan    *scan
	reading sync.Mutex
}

// Reader keeps one cursor per transcript.
type Reader struct {
	mu      sync.Mutex
	cursors map[string]*cursor
}

// Forget drops the cursor of a transcript no conversation shows any more.
func (r *Reader) Forget(path string) {
	r.mu.Lock()
	delete(r.cursors, path)
	r.mu.Unlock()
}

// Read is what the transcript says now. Reads of one transcript take turns.
func (r *Reader) Read(path string) Info {
	r.mu.Lock()
	if r.cursors == nil {
		r.cursors = map[string]*cursor{}
	}
	c := r.cursors[path]
	if c == nil {
		c = &cursor{}
		r.cursors[path] = c
	}
	r.mu.Unlock()
	c.reading.Lock()
	defer c.reading.Unlock()
	return advance(path, c)
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

func advance(path string, c *cursor) Info {
	st, err := os.Stat(path)
	if err != nil {
		*c = cursor{}
		return Info{}
	}
	if c.scan != nil && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return copyOf(c.scan.info)
	}
	fromTail := false
	if c.scan == nil || c.ino != inode(st) || st.Size() < c.offset || st.Size()-c.offset > tailBytes {
		c.ino, c.offset, c.scan = inode(st), max(0, st.Size()-tailBytes), &scan{}
		fromTail = c.offset > 0
	}
	tailStart := c.offset
	chunk := readChunk(path, c.offset, st.Size())
	start := 0
	if fromTail {
		start = bytes.IndexByte(chunk, '\n') + 1 // the tail starts inside a line
	}
	complete := bytes.LastIndexByte(chunk, '\n') + 1
	if complete > start {
		for _, raw := range bytes.Split(chunk[start:complete], []byte{'\n'}) {
			if len(raw) > 0 {
				c.scan.feed(raw)
			}
		}
	}
	c.offset += int64(complete)
	c.mtime, c.size = st.ModTime(), st.Size()
	if fromTail && c.scan.info.Goal != nil {
		// The goal was set before the part read: its age is the time of the line that set it.
		if at := goalSetAt(path, tailStart, c.scan.info.Goal.Text); at > 0 {
			c.scan.info.Goal.Since = at
		}
	}
	if fromTail && !c.scan.ultraSaid {
		// The tail never said the mode: the last word before it stands (turned off hours ago, it is off).
		c.scan.info.Ultracode, c.scan.ultraSaid = ultracodeBefore(path, tailStart), true
	}
	return copyOf(c.scan.info)
}

func copyOf(i Info) Info {
	if i.Goal != nil {
		g := *i.Goal
		i.Goal = &g
	}
	if i.Recap != nil {
		r := *i.Recap
		i.Recap = &r
	}
	return i
}

func readChunk(path string, from, to int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, to-from)
	n, _ := f.ReadAt(buf, from)
	return buf[:n]
}

// goalSetAt is the time of the last «Goal set: <text>» line before the byte, 0 when there is none. The line is short:
// it holds the goal's text and nothing else; the same words quoted inside a long line are not it.
func goalSetAt(path string, before int64, text string) int64 {
	cut := len(text)
	for i := 0; i < len(text); i++ {
		if c := text[i]; c == '"' || c == '\\' || c < 0x20 {
			cut = i
			break
		}
	}
	needle := []byte("Goal set: " + text[:min(cut, 80)])
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	const block = 4 << 20
	for end := before; end > 0; {
		from := max(0, end-block)
		buf := make([]byte, end-from)
		n, _ := f.ReadAt(buf, from)
		buf = buf[:n]
		for limit := len(buf); limit > 0; {
			i := bytes.LastIndex(buf[:limit], needle)
			if i < 0 {
				break
			}
			limit = i
			if at := shortLineTime(f, from+int64(i), len(text)); at > 0 {
				return at
			}
		}
		if from == 0 {
			break
		}
		end = from + int64(len(needle)) // a needle across two blocks
	}
	return 0
}

// shortLineTime is the timestamp of the queue-operation line the byte is in; 0 for any other line.
func shortLineTime(f *os.File, at int64, textLen int) int64 {
	const before = 512
	start := max(0, at-before)
	buf := make([]byte, int(at-start)+2*textLen+1024)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	rel := int(at - start)
	if a := bytes.LastIndexByte(buf[:rel], '\n'); a >= 0 {
		buf = buf[a+1:]
	} else if start > 0 {
		return 0 // the line began before what was read: it is a long one
	}
	b := bytes.IndexByte(buf, '\n')
	if b < 0 {
		return 0
	}
	var l struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(buf[:b], &l) != nil || l.Type != "queue-operation" {
		return 0
	}
	return millis(l.Timestamp)
}

// ultraLineMax is the longest line that may say the mode: a command's output, an attachment, a hook's. A longer one is
// a request's copy of the conversation, which quotes the words.
const ultraLineMax = 64 << 10

// ultracodeBefore is what the last line before the byte that says the mode says; Unknown when none does, or when the
// last such line started a process.
func ultracodeBefore(path string, before int64) Tri {
	f, err := os.Open(path)
	if err != nil {
		return Unknown
	}
	defer f.Close()
	const block = 4 << 20
	overlap := int64(0)
	for _, n := range ultraNeedles {
		overlap = max(overlap, int64(len(n)))
	}
	for end := before; end > 0; {
		from := max(0, end-block)
		buf := make([]byte, end-from)
		n, _ := f.ReadAt(buf, from)
		buf = buf[:n]
		for limit := len(buf); limit > 0; {
			i := lastNeedle(buf[:limit])
			if i < 0 {
				break
			}
			limit = i
			raw := lineAround(f, from+int64(i), before)
			if raw == nil {
				continue
			}
			s := &scan{}
			s.feed(raw)
			if s.ultraSaid {
				return s.info.Ultracode
			}
		}
		if from == 0 {
			break
		}
		end = from + overlap // a needle across two blocks
	}
	return Unknown
}

// lastNeedle is where the last of the needles in buf starts, -1 when none is in it.
func lastNeedle(buf []byte) int {
	at := -1
	for _, n := range ultraNeedles {
		at = max(at, bytes.LastIndex(buf, n))
	}
	return at
}

// lineAround is the whole line the byte is in, ending before the byte limit; nil for a line longer than ultraLineMax
// or one that runs on past the limit.
func lineAround(f *os.File, at, limit int64) []byte {
	start := max(0, at-ultraLineMax)
	end := min(limit, at+ultraLineMax)
	buf := make([]byte, end-start)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	rel := int(at - start)
	a := bytes.LastIndexByte(buf[:rel], '\n')
	if a < 0 && start > 0 {
		return nil
	}
	b := bytes.IndexByte(buf[rel:], '\n')
	if b < 0 {
		return nil
	}
	return buf[a+1 : rel+b]
}
