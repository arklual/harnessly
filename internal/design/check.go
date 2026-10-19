package design

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
)

// The check the design tool runs after each change: every board opened in a headless browser of this machine as a
// desktop and as a phone, its console errors, exceptions and failed requests collected, an empty page caught and a
// screenshot taken. The report and the screenshots stay next to the design (.check/); the panel shows them and an
// agent reads them. The browser is driven through the DevTools pipe: no port, no extra package.

type checkView struct {
	ID            string
	Width, Height int
	Mobile        bool
}

var checkViews = []checkView{{"desktop", 1440, 900, false}, {"phone", 390, 844, true}}

const (
	statusOK      = "ok"
	statusWarning = "warning"
	statusError   = "error"
	statusBlank   = "blank"
)

// CheckShot is one view of a board.
type CheckShot struct {
	View           string   `json:"view"`
	Status         string   `json:"status"`
	Screenshot     *string  `json:"screenshot"`
	Errors         []string `json:"errors"`
	FailedRequests []string `json:"failedRequests"`
	Ms             int64    `json:"ms"`
}

type CheckBoard struct {
	Title  string      `json:"title"`
	Path   string      `json:"path"`
	Status string      `json:"status"`
	Shots  []CheckShot `json:"shots"`
}

// CheckReport is a design's check, filling while it runs.
type CheckReport struct {
	Artifact   string       `json:"artifact"`
	StartedAt  int64        `json:"startedAt"`
	FinishedAt *int64       `json:"finishedAt"`
	Done       int          `json:"done"`
	Total      int          `json:"total"`
	Boards     []CheckBoard `json:"boards"`
	Cover      *string      `json:"cover"`
	Failure    *string      `json:"failure"`
}

func (r *CheckReport) copy() *CheckReport {
	c := *r
	c.Boards = append([]CheckBoard{}, r.Boards...)
	return &c
}

const (
	checkLoadTimeout = 20 * time.Second
	checkSettle      = 1200 * time.Millisecond
	blankRetries     = 4
	blankRetryWait   = time.Second
	clickLimit       = 12
	clickSettle      = 250 * time.Millisecond
	checkTabs        = 4
	maxLines         = 12
	maxLine          = 400
	reportName       = "report.json"
)

var chromeCandidates = []string{"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser"}

// checkState is a library's running checks and the order of its report saves.
type checkState struct {
	mu      sync.Mutex
	running map[string]*CheckReport
	saves   map[string]*sync.Mutex
}

var checkStates sync.Map

func (l *Library) checks() *checkState {
	if s, ok := checkStates.Load(l); ok {
		return s.(*checkState)
	}
	s, _ := checkStates.LoadOrStore(l, &checkState{running: map[string]*CheckReport{}, saves: map[string]*sync.Mutex{}})
	return s.(*checkState)
}

// libraryBase is where the library's server is reached; tests point it elsewhere.
var libraryBase = func(l *Library) (string, error) {
	port, err := l.EnsureServer()
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + strconv.Itoa(port), nil
}

func (l *Library) CheckDir(ref string) string { return filepath.Join(l.ArtifactDir(ref), ".check") }

var lineSpace = regexp.MustCompile(jscompat.SpaceClass + `+`)

// line is a reported text on one line, cut to 400 units.
func line(s string) string {
	return jscompat.Slice(jscompat.Trim(lineSpace.ReplaceAllString(s, " ")), maxLine)
}

func pushLine(list *[]string, v string) {
	if v == "" || len(*list) >= maxLines {
		return
	}
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}

// worstStatus: an error beats an empty page, which beats a warning.
func worstStatus(statuses []string) string {
	has := map[string]bool{}
	for _, s := range statuses {
		has[s] = true
	}
	switch {
	case has[statusError]:
		return statusError
	case has[statusBlank]:
		return statusBlank
	case has[statusWarning]:
		return statusWarning
	}
	return statusOK
}

// shotStatus: errors make a shot red, a failed request alone only yellow.
func shotStatus(errs, failed []string, blank bool) string {
	switch {
	case len(errs) > 0:
		return statusError
	case blank:
		return statusBlank
	case len(failed) > 0:
		return statusWarning
	}
	return statusOK
}

func (l *Library) findChrome() string {
	for _, c := range append([]string{l.cfg.Chrome}, chromeCandidates...) {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

type browser struct {
	cmd     *exec.Cmd
	cdp     *cdpPipe
	profile string
	exited  chan struct{}
}

func (l *Library) launchBrowser(bin string) (*browser, error) {
	profile, err := os.MkdirTemp("", l.cfg.Texts.Tag+"-check-")
	if err != nil {
		return nil, err
	}
	toChrome, ours, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	theirs, fromChrome, err := os.Pipe()
	if err != nil {
		_ = toChrome.Close()
		_ = ours.Close()
		_ = os.RemoveAll(profile)
		return nil, err
	}
	cmd := exec.Command(bin, "--headless=new", "--remote-debugging-pipe", "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check",
		"--disable-dev-shm-usage", "--hide-scrollbars", "--mute-audio", "--lang=ru-RU", "about:blank")
	cmd.ExtraFiles = []*os.File{toChrome, fromChrome}
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{toChrome, ours, theirs, fromChrome} {
			_ = f.Close()
		}
		_ = os.RemoveAll(profile)
		return nil, err
	}
	_ = toChrome.Close()
	_ = fromChrome.Close()
	b := &browser{cmd: cmd, profile: profile, exited: make(chan struct{}), cdp: newCDPPipe(ours, theirs)}
	go func() {
		_ = cmd.Wait()
		_ = theirs.Close()
		close(b.exited)
	}()
	return b, nil
}

func (b *browser) close() {
	done := make(chan struct{})
	go func() {
		_, _ = b.cdp.send("Browser.close", nil, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	b.cdp.close(fmt.Errorf("CDP closed"))
	select {
	case <-b.exited:
	case <-time.After(3 * time.Second):
	}
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	// The browser's helpers may still be flushing the profile a moment after it is gone.
	for i := 0; i < 6; i++ {
		if os.RemoveAll(b.profile) == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

const dropBar = `document.querySelectorAll('[data-tk-library-bar]').forEach((node) => node.remove())`

var (
	mediaNodes  = map[string]bool{"IMG": true, "svg": true, "SVG": true, "CANVAS": true, "VIDEO": true}
	silentNodes = map[string]bool{"SCRIPT": true, "STYLE": true, "TEMPLATE": true, "NOSCRIPT": true, "HEAD": true}
)

// pageContent counts the text and media of a whole page, through every shadow root and frame: an empty page after
// loading is a blank board, however quietly it failed.
func pageContent(root map[string]any) (text, media int) {
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		name, _ := n["nodeName"].(string)
		if silentNodes[name] {
			return
		}
		if t, _ := n["nodeType"].(float64); t == 3 {
			if v, ok := n["nodeValue"].(string); ok {
				text += jscompat.Len(jscompat.Trim(v))
			}
		}
		if mediaNodes[name] {
			media++
		}
		for _, key := range []string{"shadowRoots", "children"} {
			if arr, ok := n[key].([]any); ok {
				for _, c := range arr {
					if m, ok := c.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
		if doc, ok := n["contentDocument"].(map[string]any); ok {
			walk(doc)
		}
	}
	walk(root)
	return text, media
}

var clickPrepare = `(() => {
  window.open = () => null;
  document.addEventListener('submit', (event) => event.preventDefault(), true);
  document.addEventListener('click', (event) => {
    const link = event.target && event.target.closest ? event.target.closest('a[href]') : null;
    if (link && !link.getAttribute('href').startsWith('#')) event.preventDefault();
  }, true);
  const seen = new Set();
  const found = [];
  const walk = (root) => {
    for (const node of root.querySelectorAll('button,[role="button"],[role="switch"],[role="tab"],[role="checkbox"],input[type="checkbox"],input[type="radio"],summary')) {
      if (seen.has(node) || node.closest('[data-tk-library-bar]') || node.disabled || node.getAttribute('aria-disabled') === 'true') continue;
      const r = node.getBoundingClientRect();
      if (r.width < 4 || r.height < 4 || r.bottom < 0 || r.top > innerHeight * 3) continue;
      seen.add(node);
      found.push(node);
    }
    for (const host of root.querySelectorAll('*')) if (host.shadowRoot) walk(host.shadowRoot);
  };
  walk(document);
  window.__tkClickables = found.slice(0, ` + strconv.Itoa(clickLimit) + `);
  return window.__tkClickables.map((node) => (node.getAttribute('aria-label') || node.textContent || node.getAttribute('title') || node.tagName).replace(/\s+/g, ' ').trim().slice(0, 40));
})()`

func clickAt(i int) string {
	return fmt.Sprintf(`(() => { const node = window.__tkClickables && window.__tkClickables[%d]; if (node && node.isConnected) node.click(); })()`, i)
}

// shotErrors is the error list a shot's event handler and its clicks share.
type shotErrors struct {
	mu   sync.Mutex
	errs []string
}

func (s *shotErrors) push(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushLine(&s.errs, v)
}

func (s *shotErrors) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.errs)
}

func (s *shotErrors) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.errs...)
}

// clickThrough clicks the board's controls one by one; an error after a click is named by the control it came from.
func clickThrough(cdp *cdpPipe, session string, errs *shotErrors) {
	res, err := cdp.send("Runtime.evaluate", map[string]any{"expression": clickPrepare, "returnByValue": true}, session)
	if err != nil {
		return
	}
	var labels []any
	if r, ok := res["result"].(map[string]any); ok {
		labels, _ = r["value"].([]any)
	}
	for i, label := range labels {
		before := errs.len()
		_, _ = cdp.send("Runtime.evaluate", map[string]any{"expression": clickAt(i)}, session)
		time.Sleep(clickSettle)
		errs.mu.Lock()
		for at := before; at < len(errs.errs); at++ {
			errs.errs[at] = line("клик «" + jscompat.String(label) + "»: " + errs.errs[at])
		}
		errs.mu.Unlock()
	}
}

type shotView struct {
	Width, Height int
	Mobile        bool
}

type shotResult struct {
	Status         string
	Errors, Failed []string
	Ms             int64
	Saved          bool
}

var faviconRe = regexp.MustCompile(`favicon`)

// shoot opens a board in a tab and takes its picture; clicks false leaves its controls alone (a gallery picture).
func shoot(cdp *cdpPipe, session, url string, view shotView, file string, clicks bool) shotResult {
	errs := &shotErrors{}
	var failMu sync.Mutex
	var failed []string
	started := time.Now()
	loaded := make(chan struct{})
	var once sync.Once
	urls := map[string]string{}
	stop := cdp.on(func(ev cdpEvent) {
		if ev.SessionID != session {
			return
		}
		p := ev.Params
		switch ev.Method {
		case "Page.loadEventFired":
			once.Do(func() { close(loaded) })
		case "Runtime.exceptionThrown":
			d, _ := p["exceptionDetails"].(map[string]any)
			msg := ""
			desc, hasDesc := "", false
			if ex, ok := d["exception"].(map[string]any); ok {
				desc, hasDesc = ex["description"].(string)
			}
			if hasDesc {
				msg, _, _ = strings.Cut(desc, "\n")
			} else if t, ok := d["text"]; ok && t != nil {
				msg = jscompat.String(t)
			}
			errs.push(line(msg))
		case "Runtime.consoleAPICalled":
			if p["type"] != "error" {
				return
			}
			args, ok := p["args"].([]any)
			if !ok {
				errs.push(line(""))
				return
			}
			parts := make([]string, len(args))
			for i, a := range args {
				m, _ := a.(map[string]any)
				v := m["value"]
				if v == nil {
					v = m["description"]
				}
				if v != nil {
					parts[i] = jscompat.String(v)
				}
			}
			errs.push(line(strings.Join(parts, " ")))
		case "Log.entryAdded":
			e, _ := p["entry"].(map[string]any)
			t, _ := e["text"].(string)
			if e["level"] == "error" && !strings.Contains(t, "Failed to load resource") {
				errs.push(line(jsStringOr(e["text"])))
			}
		case "Network.requestWillBeSent":
			req, _ := p["request"].(map[string]any)
			u := ""
			if v, ok := req["url"]; ok && v != nil {
				u = jscompat.String(v)
			}
			failMu.Lock()
			urls[jscompat.String(p["requestId"])] = u
			failMu.Unlock()
		case "Network.loadingFailed":
			if jscompat.Truthy(p["canceled"]) {
				return
			}
			failMu.Lock()
			pushLine(&failed, line(jsUndef(p, "errorText")+" "+urls[jscompat.String(p["requestId"])]))
			failMu.Unlock()
		case "Network.responseReceived":
			resp, _ := p["response"].(map[string]any)
			status, _ := resp["status"].(float64)
			u, _ := resp["url"].(string)
			if status >= 400 && !faviconRe.MatchString(u) {
				failMu.Lock()
				pushLine(&failed, line(jsUndef(resp, "status")+" "+jsUndef(resp, "url")))
				failMu.Unlock()
			}
		}
	})
	defer stop()
	result := func(status string, saved bool) shotResult {
		failMu.Lock()
		f := append([]string{}, failed...)
		failMu.Unlock()
		e := errs.list()
		if status == "" {
			status = shotStatus(e, f, false)
		}
		return shotResult{Status: status, Errors: e, Failed: f, Ms: time.Since(started).Milliseconds(), Saved: saved}
	}
	fail := func(err error) shotResult {
		errs.push(line(err.Error()))
		return result(statusError, false)
	}
	if _, err := cdp.send("Emulation.setDeviceMetricsOverride", map[string]any{"width": view.Width, "height": view.Height, "deviceScaleFactor": 1, "mobile": view.Mobile}, session); err != nil {
		return fail(err)
	}
	if _, err := cdp.send("Page.navigate", map[string]any{"url": url}, session); err != nil {
		return fail(err)
	}
	select {
	case <-loaded:
	case <-time.After(checkLoadTimeout):
	}
	time.Sleep(checkSettle)
	if _, err := cdp.send("Runtime.evaluate", map[string]any{"expression": dropBar}, session); err != nil {
		return fail(err)
	}
	// A heavy board under four busy tabs may still be drawing: blank only if it stays empty a few seconds more.
	blank := true
	for attempt := 0; attempt <= blankRetries && blank; attempt++ {
		if attempt > 0 {
			time.Sleep(blankRetryWait)
		}
		dom, err := cdp.send("DOM.getDocument", map[string]any{"depth": -1, "pierce": true}, session)
		if err != nil {
			return fail(err)
		}
		root, _ := dom["root"].(map[string]any)
		if root == nil {
			return fail(fmt.Errorf("Cannot read properties of undefined (reading 'nodeName')"))
		}
		text, media := pageContent(root)
		blank = text < 5 && media == 0
	}
	// Four tabs share one browser: a capture that misses its turn once gets another.
	capture := func() (map[string]any, error) {
		return cdp.send("Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 72}, session)
	}
	pic, err := capture()
	if err != nil {
		if pic, err = capture(); err != nil {
			return fail(err)
		}
	}
	data, _ := pic["data"].(string)
	if data != "" {
		raw, derr := base64.StdEncoding.DecodeString(data)
		if derr != nil {
			return fail(derr)
		}
		if err := os.WriteFile(file, raw, 0o666); err != nil {
			return fail(err)
		}
	}
	if !view.Mobile && !blank && clicks {
		clickThrough(cdp, session, errs)
	}
	failMu.Lock()
	f := append([]string{}, failed...)
	failMu.Unlock()
	e := errs.list()
	return shotResult{Status: shotStatus(e, f, blank), Errors: e, Failed: f, Ms: time.Since(started).Milliseconds(), Saved: data != ""}
}

func jsStringOr(v any) string {
	if v == nil {
		return ""
	}
	return jscompat.String(v)
}

// jsUndef is `${o.k}` where an absent key reads "undefined".
func jsUndef(o map[string]any, k string) string {
	v, ok := o[k]
	if !ok {
		return "undefined"
	}
	return jscompat.String(v)
}

// reportJSON is JSON.stringify(report, null, 2): the compact JavaScript bytes, indented (Indent leaves strings alone).
func reportJSON(r *CheckReport) []byte {
	var buf bytes.Buffer
	_ = json.Indent(&buf, nodejson.Marshal(r), "", "  ")
	return buf.Bytes()
}

// saveReport writes the report through a temporary file; the saves of one design queue, so the last one stays.
func (l *Library) saveReport(r *CheckReport) error {
	s := l.checks()
	s.mu.Lock()
	m := s.saves[r.Artifact]
	if m == nil {
		m = &sync.Mutex{}
		s.saves[r.Artifact] = m
	}
	snap := r.copy()
	s.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	dir := l.CheckDir(snap.Artifact)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	tmp := filepath.Join(dir, reportName+".tmp")
	if err := os.WriteFile(tmp, reportJSON(snap), 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, reportName))
}

func shotName(at int, view string) string { return fmt.Sprintf("%03d-%s.jpg", at+1, view) }

type checkTarget struct {
	board Board
	at    int
}

func strp(s string) *string { return &s }

// run checks the targets with four tabs of one browser; kept holds the last results of the boards not checked this
// time, by place in the design. A check of every board starts from an empty folder.
func (l *Library) runCheck(report *CheckReport, all []Board, targets []checkTarget, kept []*CheckBoard) {
	s := l.checks()
	defer func() {
		s.mu.Lock()
		delete(s.running, report.Artifact)
		s.mu.Unlock()
	}()
	finish := func() {
		s.mu.Lock()
		now := nowMs()
		report.FinishedAt = &now
		s.mu.Unlock()
		_ = l.saveReport(report)
	}
	dir := l.CheckDir(report.Artifact)
	if len(targets) == len(all) {
		_ = os.RemoveAll(dir)
	}
	_ = os.MkdirAll(dir, 0o777)
	bin := l.findChrome()
	if bin == "" {
		s.mu.Lock()
		report.Failure = strp("На машине нет Chrome: поставьте google-chrome или задайте RUNTIME_DESIGN_CHROME")
		s.mu.Unlock()
		finish()
		return
	}
	fatal := func(err error) {
		s.mu.Lock()
		report.Failure = strp("Проверка оборвалась: " + line(err.Error()))
		s.mu.Unlock()
	}
	base, err := libraryBase(l)
	if err != nil {
		fatal(err)
		finish()
		return
	}
	b, err := l.launchBrowser(bin)
	if err != nil {
		fatal(err)
		finish()
		return
	}
	results := append([]*CheckBoard{}, kept...)
	var next int
	var nextMu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	worker := func() error {
		t, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
		if err != nil {
			return err
		}
		a, err := b.cdp.send("Target.attachToTarget", map[string]any{"targetId": t["targetId"], "flatten": true}, "")
		if err != nil {
			return err
		}
		session, _ := a["sessionId"].(string)
		for _, d := range []string{"Page", "Runtime", "Log", "Network", "DOM"} {
			if _, err := b.cdp.send(d+".enable", nil, session); err != nil {
				return err
			}
		}
		for {
			nextMu.Lock()
			index := next
			next++
			nextMu.Unlock()
			if index >= len(targets) {
				return nil
			}
			tg := targets[index]
			var shots []CheckShot
			for _, v := range checkViews {
				name := shotName(tg.at, v.ID)
				shot := shoot(b.cdp, session, base+BoardHref(report.Artifact, tg.board.Path), shotView{v.Width, v.Height, v.Mobile}, filepath.Join(dir, name), true)
				var ss *string
				if shot.Saved {
					ss = strp(name)
				}
				shots = append(shots, CheckShot{View: v.ID, Status: shot.Status, Screenshot: ss, Errors: shot.Errors, FailedRequests: shot.Failed, Ms: shot.Ms})
				s.mu.Lock()
				report.Done++
				s.mu.Unlock()
			}
			statuses := make([]string, len(shots))
			for i, sh := range shots {
				statuses[i] = sh.Status
			}
			s.mu.Lock()
			results[tg.at] = &CheckBoard{Title: tg.board.Title, Path: tg.board.Path, Status: worstStatus(statuses), Shots: shots}
			// The report keeps the boards' own order while the tabs finish them out of order.
			report.Boards = report.Boards[:0:0]
			for _, r := range results {
				if r != nil {
					report.Boards = append(report.Boards, *r)
				}
			}
			if tg.at == 0 {
				report.Cover = shots[0].Screenshot
			}
			s.mu.Unlock()
			_ = l.saveReport(report)
		}
	}
	tabs := checkTabs
	if len(targets) < tabs {
		tabs = len(targets)
	}
	var errMu sync.Mutex
	for i := 0; i < tabs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := worker(); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		fatal(firstErr)
	}
	b.close()
	finish()
}

// parseReport reads a saved report leniently into its shape.
func parseReport(raw []byte) *CheckReport {
	var r CheckReport
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	return &r
}

// ReadCheckReport is the running check of a design, else its last saved report: the value to answer with (the saved
// document as it is on disk), and its shape (nil when there is none).
func (l *Library) ReadCheckReport(ref string) (any, *CheckReport) {
	s := l.checks()
	s.mu.Lock()
	if live := s.running[ref]; live != nil {
		snap := live.copy()
		s.mu.Unlock()
		return snap, snap
	}
	s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(l.CheckDir(ref), reportName))
	if err != nil {
		return nil, nil
	}
	doc, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(b)))
	if err != nil {
		return nil, nil
	}
	r := parseReport(b)
	if r == nil {
		r = &CheckReport{Artifact: ref}
	}
	return doc, r
}

// StartCheck starts a check of every board, or of those named (by path or title), and returns at once with the report
// as it fills; a check already running for the design is returned instead. Nil: no such design or board.
func (l *Library) StartCheck(ref string, only []string) *CheckReport {
	if !IsLibraryID(ref) {
		return nil
	}
	s := l.checks()
	s.mu.Lock()
	if live := s.running[ref]; live != nil {
		snap := live.copy()
		s.mu.Unlock()
		return snap
	}
	s.mu.Unlock()
	a := l.ReadArtifact(ref)
	if a == nil {
		return nil
	}
	named := func(b Board) bool {
		if len(only) == 0 {
			return true
		}
		for _, o := range only {
			if o == b.Path || o == b.Title {
				return true
			}
		}
		return false
	}
	all := a.Boards
	var targets []checkTarget
	targeted := map[int]bool{}
	for at, b := range all {
		if named(b) {
			targets = append(targets, checkTarget{b, at})
			targeted[at] = true
		}
	}
	if len(targets) == 0 {
		return nil
	}
	// A check of some boards keeps the last results of the rest, while they are still at the place their files name.
	var previous *CheckReport
	if len(targets) < len(all) {
		_, previous = l.ReadCheckReport(ref)
	}
	kept := make([]*CheckBoard, len(all))
	for at, b := range all {
		if targeted[at] || previous == nil {
			continue
		}
		for i := range previous.Boards {
			last := &previous.Boards[i]
			if last.Path != b.Path {
				continue
			}
			ok := true
			for _, sh := range last.Shots {
				if sh.Screenshot != nil && *sh.Screenshot != shotName(at, sh.View) {
					ok = false
				}
			}
			if ok {
				c := *last
				kept[at] = &c
			}
			break
		}
	}
	report := &CheckReport{Artifact: ref, StartedAt: nowMs(), Total: len(targets) * len(checkViews), Boards: []CheckBoard{}}
	for _, k := range kept {
		if k != nil {
			report.Boards = append(report.Boards, *k)
		}
	}
	if len(kept) > 0 && kept[0] != nil && previous != nil {
		report.Cover = previous.Cover
	}
	s.mu.Lock()
	if live := s.running[ref]; live != nil {
		snap := live.copy()
		s.mu.Unlock()
		return snap
	}
	s.running[ref] = report
	snap := report.copy()
	s.mu.Unlock()
	go l.runCheck(report, all, targets, kept)
	return snap
}

var shotFileRe = regexp.MustCompile(`^\d{3}-(desktop|phone)\.jpg$`)

// checkShotPath is a check's screenshot file, confined to the design's check folder; "" when the names are not one.
func (l *Library) checkShotPath(ref, file string) string {
	if !IsLibraryID(ref) || !shotFileRe.MatchString(file) {
		return ""
	}
	return filepath.Join(l.CheckDir(ref), file)
}

func checkSummary(r *CheckReport) *CheckSummary {
	s := &CheckSummary{FinishedAt: r.FinishedAt, Done: r.Done, Total: r.Total, Failure: r.Failure}
	for _, b := range r.Boards {
		switch b.Status {
		case statusError, statusBlank:
			s.Broken++
		case statusWarning:
			s.Warnings++
		}
	}
	return s
}

// checkShotURL is a screenshot's address for the panel, versioned by the check so a new one is never a cached one.
func (l *Library) checkShotURL(r *CheckReport, file string) string {
	return l.cfg.PlatformAPI + "/design/library/shot?artifact=" + EncodeURIComponent(r.Artifact) + "&file=" + EncodeURIComponent(file) +
		"&v=" + strconv.FormatInt(r.StartedAt, 10)
}

// AttachChecks gives each design the cover and the outcome of its last check, if it was ever checked.
func (l *Library) AttachChecks(artifacts []*Artifact) {
	for _, a := range artifacts {
		_, r := l.ReadCheckReport(a.ID)
		if r == nil {
			continue
		}
		a.Cover = nil
		if r.Cover != nil && *r.Cover != "" {
			a.Cover = strp(l.checkShotURL(r, *r.Cover))
		}
		a.Check = checkSummary(r)
	}
}

// AgentShot is a shot as an agent reads it: the screenshot as a file it can open.
type AgentShot = CheckShot

type AgentBoard struct {
	Title  string      `json:"title"`
	Path   string      `json:"path"`
	Status string      `json:"status"`
	Shots  []AgentShot `json:"shots"`
}

// AgentReport is what /api/cli/design answers about a check.
type AgentReport struct {
	Artifact string       `json:"artifact"`
	Finished bool         `json:"finished"`
	Failure  *string      `json:"failure"`
	Boards   []AgentBoard `json:"boards"`
}

// ForAgent is a report as an agent reads it: every board and view with its status, problems and screenshot file.
func (l *Library) ForAgent(r *CheckReport) AgentReport {
	dir := l.CheckDir(r.Artifact)
	out := AgentReport{Artifact: r.Artifact, Finished: r.FinishedAt != nil, Failure: r.Failure, Boards: []AgentBoard{}}
	for _, b := range r.Boards {
		ab := AgentBoard{Title: b.Title, Path: b.Path, Status: b.Status, Shots: []AgentShot{}}
		for _, sh := range b.Shots {
			if sh.Screenshot != nil {
				sh.Screenshot = strp(filepath.Join(dir, *sh.Screenshot))
			}
			ab.Shots = append(ab.Shots, sh)
		}
		out.Boards = append(out.Boards, ab)
	}
	return out
}
