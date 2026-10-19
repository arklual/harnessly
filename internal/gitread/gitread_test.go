package gitread

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJSHelpers(t *testing.T) {
	if got := jsTrim("\u0085x\ufeff \u2028"); got != "\u0085x" {
		t.Errorf("trim %q", got)
	}
	for in, want := range map[string]float64{" -12abc": -12, "\u00a012": 12, "+7": 7, "0012": 12} {
		if v, ok := jsParseInt(in); !ok || v != want {
			t.Errorf("parseInt(%q) = %v %v", in, v, ok)
		}
	}
	for _, in := range []string{"x", "", "\u008512", "-"} {
		if _, ok := jsParseInt(in); ok {
			t.Errorf("parseInt(%q) should be NaN", in)
		}
	}
	if jsNumber(1e20) != "100000000000000000000" || jsNumber(1e21) != "1e+21" || jsNumber(12) != "12" {
		t.Errorf("jsNumber")
	}
	if jsLength("a😀") != 3 || jsPrefix("a😀b", 3) != "a😀" || jsPrefix("a😀b", 2) != "a\ufffd" || jsPrefix("abc", 7) != "abc" {
		t.Errorf("utf16 slicing")
	}
	if i := jsLastIndexOf("😀\ndiff --git a\ndiff --git b", "\ndiff --git", 10); i != 2 {
		t.Errorf("lastIndexOf %d", i)
	}
	if jsLastIndexOf("abc", "\ndiff", 2) != -1 {
		t.Errorf("lastIndexOf absent")
	}
	// Node's decoder: one replacement per maximal invalid subsequence.
	got := decodeUTF8([]byte{0x61, 0xe2, 0x82, 0x62, 0xff, 0xf0, 0x9f, 0x98, 0x63, 0xed, 0xa0, 0x80})
	if got != "a\ufffdb\ufffd\ufffdc\ufffd\ufffd\ufffd" {
		t.Errorf("decode %q", got)
	}
}

func TestParsePorcelain(t *testing.T) {
	s, m, u := parsePorcelain("M  a\n M b\nMM c\n?? d\n?? e\nA  f\nR  g -> h\n D i\n")
	if s != 4 || m != 3 || u != 2 {
		t.Fatalf("%d %d %d", s, m, u)
	}
}

func TestParseNumstatAndAheadBehind(t *testing.T) {
	i, d := parseNumstat("3\t1\ta\n-\t-\tbin\n10\t0\tb\n")
	if i != 13 || d != 1 {
		t.Fatalf("%d %d", i, d)
	}
	a, b := parseAheadBehind("2\t5\n")
	if a != 2 || b != 5 {
		t.Fatalf("%d %d", a, b)
	}
	if a, b := parseAheadBehind(""); a != 0 || b != 0 {
		t.Fatal("empty")
	}
	if countLines("") != 0 || countLines("stash@{0}: x\nstash@{1}: y\n") != 2 {
		t.Fatal("countLines")
	}
}

func TestParseRecentCommits(t *testing.T) {
	out := "__COMMIT__aaaaaaaaaa|Ann|1700000000|p1|fix: a|b\n\n 2 files changed, 5 insertions(+), 1 deletion(-)\n" +
		"__COMMIT__bbbbbbbbbb|Bob|1700000001|p1 p2|Merge\n"
	got := parseRecentCommits(out)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	// The stat line sits after a blank line, where the old parser never looked: the counts stay zero.
	if got[0].Subject != "fix: a|b" || got[0].ShortHash != "aaaaaaa" || *got[0].Timestamp != 1700000000000 || got[0].FilesChanged != 0 {
		t.Errorf("%+v", got[0])
	}
	if !got[1].IsMerge || got[0].IsMerge {
		t.Errorf("merge flags")
	}
	withStat := parseRecentCommits("__COMMIT__c|C|x|p|s\n 1 file changed, 2 insertions(+)")
	if withStat[0].FilesChanged != 1 || withStat[0].Insertions != 2 || withStat[0].Deletions != 0 || withStat[0].Timestamp != nil {
		t.Errorf("%+v", withStat[0])
	}
	if got := parseRecentCommits("__COMMIT__a|b|c|d|e\n__COMMIT__broken"); len(got) != 0 {
		t.Errorf("a broken block empties the list: %+v", got)
	}
}

func TestParseLogAndMeta(t *testing.T) {
	out := "h1\x1fp1\x1fAnn\x1fa@x\x1f1700000000\x1fsub\x1e\nh2\x1fp1 p2\x1fBob\x1fb@x\x1f1700000001\x1fm\x1fx\x1e\n"
	got, err := parseLog(out)
	if err != nil || len(got) != 2 || got[1].Subject != "m\x1fx" || !got[1].IsMerge || len(got[0].Parents) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := parseLog("lonely\x1e"); err == nil {
		t.Fatal("a record without fields must fail")
	}
	meta, ok := parseCommitMeta("abcdef0123\x1f\x1fAnn\x1fa@x\x1f1700000000\x1fsubject\x1fbody line\n\n")
	if !ok || meta.Body != "body line" || len(meta.Parents) != 0 || meta.Parents == nil || meta.ShortHash != "abcdef0" {
		t.Fatalf("%+v", meta)
	}
	if _, ok := parseCommitMeta("a\x1fb"); ok {
		t.Fatal("short meta")
	}
	if h := parseHeadCommit("h\x1fs\x1fa\x1f17\n"); h == nil || *h.Timestamp != 17000 {
		t.Fatalf("%+v", h)
	}
	if parseHeadCommit("h\x1fs") != nil {
		t.Fatal("short head")
	}
}

func TestLimits(t *testing.T) {
	for _, c := range []struct {
		l, s  string
		limit int
		skip  string
	}{
		{"", "", 50, ""},
		{"10", "5", 10, "5"},
		{"9999", "-3", 500, ""},
		{"0", "0", 50, ""},
		{"7abc", "1e3", 7, "1"},
	} {
		limit, skip := logLimits(c.l, c.s)
		if limit != c.limit || skip != c.skip {
			t.Errorf("%+v: %d %q", c, limit, skip)
		}
	}
	if got := untrackedPaths("?? a b\n M c\n?? d/\n"); !reflect.DeepEqual(got, []string{"a b", "d/"}) {
		t.Errorf("%v", got)
	}
}

type fakeTmux struct{ cwd string }

func (f fakeTmux) HasSession(name string) bool { return name == "s" }
func (f fakeTmux) PanePID(string) int          { return 0 }
func (f fakeTmux) PanePath(name string) string {
	if name == "s" {
		return f.cwd
	}
	return ""
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRoutesOnARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("fresh\n"), 0o644)
	head := gitIn(t, dir, "rev-parse", "HEAD")

	s := New(fakeTmux{cwd: dir})
	q := func(kv ...string) url.Values {
		v := url.Values{}
		for i := 0; i < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	if st, v := s.Branch(q("tmuxSession", "s")); st != 200 || *v.(BranchBody).Branch != "main" {
		t.Fatalf("branch %d %+v", st, v)
	}
	if st, v := s.Branch(q()); st != 400 || v.(errBody).Message == "" {
		t.Fatalf("branch without session %d %+v", st, v)
	}
	if st, _ := s.Branch(q("tmuxSession", "nope")); st != 404 {
		t.Fatalf("branch of a missing session %d", st)
	}
	st, v := s.Status(q("tmuxSession", "s", "force", "1"))
	status := v.(StatusBody).Status
	if st != 200 || status.Modified != 1 || status.Untracked != 1 || status.Insertions != 1 || len(status.RecentCommits) != 1 {
		t.Fatalf("status %d %+v", st, status)
	}
	st, v = s.Diff(q("session", "s"))
	d, ok := v.(Diff)
	if st != 200 || !ok || !strings.Contains(d.Diff, "+two") || !strings.Contains(d.Diff, "+fresh") || d.Branch != "main" ||
		d.Upstream != nil || d.HeadCommit == nil || d.HeadCommit.Hash != head || len(d.Hash) != 16 {
		t.Fatalf("diff %d %+v", st, v)
	}
	if st, v := s.Diff(q("session", "s", "hashOnly", "true")); st != 200 || v.(DiffHash).Hash != d.Hash {
		t.Fatalf("hash %d %+v", st, v)
	}
	if st, _ := s.Diff(q("session", "x")); st != 404 {
		t.Fatalf("diff of a missing session %d", st)
	}
	if st, v := s.CommitDiff(q("session", "s", "hash", head[:8])); st != 200 || !strings.Contains(v.(CommitDiff).Diff, "+one") {
		t.Fatalf("commit diff %d %+v", st, v)
	}
	if st, v := s.CommitDiff(q("session", "s", "hash", "zz")); st != 400 || v.(errBody).Error != "Invalid hash format" {
		t.Fatalf("bad hash %d %+v", st, v)
	}
	if st, v := s.GitLog(q("session", "s")); st != 200 || v.(Log).Head != head || len(v.(Log).Commits) != 1 {
		t.Fatalf("log %d %+v", st, v)
	}
	if st, v := s.GitLog(q("session", "s", "skip", "1")); st != 200 || v.(Log).Head != "" || len(v.(Log).Commits) != 0 {
		t.Fatalf("log page %d %+v", st, v)
	}
	if st, v := CommitChanges(q("root", dir, "sha", head)); st != 200 || !strings.Contains(v.(Changes).Diff, "+one") {
		t.Fatalf("changes %d %+v", st, v)
	}
	if st, _ := CommitChanges(q("root", filepath.Join(dir, "sub"), "sha", head)); st != 404 {
		t.Fatalf("changes outside the root %d", st)
	}
	if st, _ := CommitChanges(url.Values{"root": {dir, dir}, "sha": {head}}); st != 404 {
		t.Fatalf("repeated root %d", st)
	}

	plain := t.TempDir()
	s = New(fakeTmux{cwd: plain})
	if st, v := s.Branch(q("tmuxSession", "s")); st != 200 || v.(BranchBody).Branch != nil {
		t.Fatalf("branch outside a repo %d %+v", st, v)
	}
	if st, v := s.Diff(q("session", "s")); st != 200 || v.(DiffNotRepo).IsGitRepo {
		t.Fatalf("diff outside a repo %d %+v", st, v)
	}
	if st, v := s.GitLog(q("session", "s")); st != 200 || v.(LogNotRepo).Commits == nil {
		t.Fatalf("log outside a repo %d %+v", st, v)
	}
}

func TestChangesTruncation(t *testing.T) {
	// The cut keeps whole files: up to the last file header that starts before the limit.
	d := "diff --git a\n" + strings.Repeat("x", maxChangesDiff-100) + "\ndiff --git b\n" + strings.Repeat("y", 200)
	cut := jsLastIndexOf(d, "\ndiff --git", maxChangesDiff) + 1
	if got := jsPrefix(d, cut); !strings.HasSuffix(got, "x\n") || strings.Contains(got, "diff --git b") {
		t.Fatalf("cut at %d", cut)
	}
}
