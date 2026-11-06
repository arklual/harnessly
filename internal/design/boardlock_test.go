package design

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const lockedTemplate = `<!doctype html><html><head><script src="./support.js"></script><script>alert(1)</script></head><body><x-dc>
<main onclick="steal()" data-hl-key="root">
  <h1 data-hl-key="title" data-hl-edit="text">Title</h1>
  <p data-hl-field="note">Note</p>
  <a data-hl-field="link" data-hl-attr="href" href="javascript:evil()">Link</a>
  <p data-hl-key="fixed">Fixed words</p>
  <iframe srcdoc="<script>x</script>" src="https://ok.example/"></iframe>
</main></x-dc></body></html>`

func lockedRig(t *testing.T) (*Library, Page, string) {
	t.Helper()
	l, _, dir := docRig(t)
	p, err := l.DocAddBoard(docProject, "Locked", []byte(lockedTemplate), &DocTemplate{App: "cashback", ID: "templates/card", Version: "1.0.0", Lock: TemplateLockStrict}, DocActor{Subject: "Cashback"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return l, p, filepath.Join(dir, ProjectDirname, pageFileOf(p.File))
}

func TestAddBoardCleansTemplate(t *testing.T) {
	_, _, file := lockedRig(t)
	got, _ := os.ReadFile(file)
	s := string(got)
	for _, bad := range []string{"alert(1)", "onclick", "steal", "javascript:", "srcdoc"} {
		if strings.Contains(s, bad) {
			t.Errorf("%q survived: %s", bad, s)
		}
	}
	for _, good := range []string{`<script src="./support.js"></script>`, "data-hl-key=\"title\"", `src="https://ok.example/"`, "Fixed words"} {
		if !strings.Contains(s, good) {
			t.Errorf("%q lost: %s", good, s)
		}
	}
}

func TestLockedBoardPatch(t *testing.T) {
	l, p, file := lockedRig(t)
	if _, ok := p.Template(); !ok {
		if m := l.ReadModel(docProject); m == nil || func() bool { q, _ := m.Page(p.ID); _, ok := q.Template(); return !ok }() {
			t.Fatal("the record is not in meta")
		}
	}
	nodes, err := l.DocNodes(docProject, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]string{}
	for _, n := range nodes {
		byText[n.Own] = n.ID
	}
	now := time.Now()
	if _, err := l.DocPatch(docProject, []DocOp{{Op: "setText", Node: byText["Title"], Value: "New"}}, DocActor{Subject: "App"}, now); err != nil {
		t.Fatalf("an editable text: %v", err)
	}
	if _, err := l.DocPatch(docProject, []DocOp{{Op: "setText", Node: byText["Note"], Value: "New note"}}, DocActor{Subject: "App"}, now); err != nil {
		t.Fatalf("a field: %v", err)
	}
	var de *DocError
	_, err = l.DocPatch(docProject, []DocOp{{Op: "setText", Node: byText["Fixed words"], Value: "Mine"}}, DocActor{Subject: "App"}, now)
	if !errors.As(err, &de) || de.Code != DocErrLocked || !strings.Contains(de.Msg, "fixed") {
		t.Fatalf("a closed text: %v", err)
	}
	if strings.Contains(readFile(t, file), "Mine") {
		t.Error("written despite the lock")
	}
	// The editor's hand edit goes through the same check.
	m := l.ReadModel(docProject)
	if m.PageOfFile(pageFileOf(p.File)) == "" {
		t.Fatal("no page")
	}
	// An open edit mark in the file does not open anything: the record decides.
	raw := readFile(t, file)
	forged := strings.Replace(raw, `data-hl-key="fixed"`, `data-hl-key="fixed" data-hl-edit="text"`, 1)
	if err := l.checkBoardLock(p2(t, l, p.ID), forged); err == nil {
		t.Error("a forged data-hl-edit opened a closed part")
	}
	// The agent's hook closes the file.
	if r := l.BoardLockReason(docProject, file); r == "" {
		t.Error("the board file is open to the agent")
	}
	if r := l.BoardLockReason(docProject, filepath.Join(l.MetaDir(docProject), MetaPagesDirname, p.ID+".json")); r == "" {
		t.Error("the record is open to the agent")
	}
	// A copy is locked as well; an unbinding frees the board.
	if _, err := l.EditBoards(docProject, BoardEdit{Action: "duplicate", Path: strp(pageFileOf(p.File))}); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	locked := 0
	for _, q := range l.ReadModel(docProject).Pages {
		if tp, ok := q.Template(); ok && tp.Locked() {
			locked++
		}
	}
	if locked != 2 {
		t.Errorf("locked pages after a copy: %d", locked)
	}
	if err := l.UnbindTemplate(docProject, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DocPatch(docProject, []DocOp{{Op: "setText", Node: byText["Fixed words"], Value: "Mine"}}, DocActor{Subject: "App"}, now); err != nil {
		t.Fatalf("after unbinding: %v", err)
	}
}

func p2(t *testing.T, l *Library, id string) Page {
	t.Helper()
	p, ok := l.ReadModel(docProject).Page(id)
	if !ok {
		t.Fatal("no page")
	}
	return p
}

func TestLockedBoardHandEditAndRestore(t *testing.T) {
	l, p, file := lockedRig(t)
	nodes, _ := l.DocNodes(docProject, p.ID)
	var fixed DocNode
	for _, n := range nodes {
		if n.Own == "Fixed words" {
			fixed = n
		}
	}
	_, _, err := l.patchDeckBoard(DeckPatchRequest{Artifact: docProject, Page: p.ID, Tpl: fixed.Tpl, Op: DeckPatchText, Was: strp("Fixed words"), HTML: strp("Mine")}, time.Now())
	var pe *deckPatchErr
	if !errors.As(err, &pe) || pe.code != DeckPatchLocked {
		t.Fatalf("hand edit of a closed part: %v", err)
	}
	// A version whose closed part differs does not come back over a locked board.
	good := readFile(t, file)
	bad := strings.Replace(good, "Fixed words", "Other words", 1)
	if err := os.WriteFile(file, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.snapshotNow("rt", nil, "bad", time.Now()); err != nil {
		t.Fatal(err)
	}
	repo, err := l.ensureRepo()
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.git(repo, "rev-parse", "HEAD")
	if err != nil || res.code != 0 {
		t.Fatalf("rev-parse: %v %v", err, res)
	}
	badRev := strings.TrimSpace(res.stdout)
	if err := os.WriteFile(file, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.snapshotNow("rt", nil, "good", time.Now()); err != nil {
		t.Fatal(err)
	}
	_, err = l.restoreBoard(docProject, pageFileOf(p.File), badRev, restoreActor{runtime: "rt", line: "me"}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "Locked") && !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "закрыта") {
		t.Fatalf("restore over a lock: %v", err)
	}
	if readFile(t, file) != good {
		t.Error("the board changed")
	}
	if tp := p2(t, l, p.ID); func() bool { _, ok := tp.Template(); return !ok }() {
		t.Error("the record is gone")
	}
}

func TestGuardClosesLockedBoard(t *testing.T) {
	l, p, file := lockedRig(t)
	in := `{"tool_name":"Write","tool_input":{"file_path":"` + file + `"},"cwd":"` + filepath.Dir(file) + `"}`
	root := filepath.Dir(l.ProjectDir(docProject))
	if out := GuardHook([]byte(in), "harnessly", GuardEnv{LibraryDir: root}); !strings.Contains(string(out), "deny") {
		t.Errorf("a write of a locked board: %s", out)
	}
	other := strings.Replace(in, file, filepath.Join(filepath.Dir(file), "Two.dc.html"), 1)
	if out := GuardHook([]byte(other), "harnessly", GuardEnv{LibraryDir: root}); len(out) != 0 {
		t.Errorf("a write of an ordinary board: %s", out)
	}
	_ = p
}

func TestCleanTemplateBoardBypasses(t *testing.T) {
	cases := map[string]struct{ in, gone string }{
		"self-closing script": {`<x-dc><script/><img src=x onerror=alert(1)><script>alert(2)</script></x-dc>`, "alert"},
		"style mutation":      {`<x-dc><style><img src=x onerror=alert(1)></style><textarea></textarea><img src=x onerror=alert(3)></x-dc>`, "alert(3)"},
		"noscript":            {`<x-dc><noscript><p title="</noscript><img src=x onerror=alert(1)>"></noscript></x-dc>`, "onerror"},
		"svg style":           {`<x-dc><svg><style><a id="</style><img src=x onerror=alert(1)>"></style></svg></x-dc>`, "onerror"},
		"math":                {`<x-dc><math><mtext><table><mglyph><style><img src=x onerror=alert(1)>`, "onerror"},
		"foreignObject":       {`<x-dc><svg><foreignObject><img src=x onerror=alert(1)></foreignObject></svg></x-dc>`, "onerror"},
		"base":                {`<head><base href="https://evil.example/"><script src="./support.js"></script></head><x-dc>x</x-dc>`, "evil.example"},
		"svg animate":         {`<x-dc><svg><a><animate attributeName=href values="javascript:alert(1)"/><set attributeName=href to="javascript:alert(1)"/></a></svg></x-dc>`, "javascript"},
		"object data":         {`<x-dc><object data="javascript:alert(1)"></object><embed src="javascript:alert(1)"></x-dc>`, "javascript"},
		"meta refresh":        {`<head><meta http-equiv="refresh" content="0;url=javascript:alert(1)"></head><x-dc>x</x-dc>`, "refresh"},
		"xlink":               {`<x-dc><svg><use xlink:href="javascript:alert(1)"/></svg></x-dc>`, "javascript"},
		"engine with body":    {`<head><script src="./support.js">alert(1)</script></head><x-dc>x</x-dc>`, "alert"},
		"engine with extra":   {`<head><script src="./support.js" onload="alert(1)"></script></head><x-dc>x</x-dc>`, "alert"},
		"foreign script src":  {`<head><script src="https://evil.example/x.js"></script></head><x-dc>x</x-dc>`, "evil.example"},
	}
	for name, c := range cases {
		out, err := CleanTemplateBoard([]byte(c.in))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Contains(string(out), c.gone) {
			t.Errorf("%s: %q survived: %s", name, c.gone, out)
		}
		again, err := CleanTemplateBoard(out)
		if err != nil || string(again) != string(out) {
			t.Errorf("%s: not a fixpoint", name)
		}
	}
	out, _ := CleanTemplateBoard([]byte(`<head><script src="./support.js"></script></head><x-dc><p data-hl-key="a">x</p></x-dc>`))
	if !strings.Contains(string(out), `<script src="./support.js"></script>`) || !strings.Contains(string(out), `data-hl-key="a"`) {
		t.Errorf("the engine or the markup was lost: %s", out)
	}
}
