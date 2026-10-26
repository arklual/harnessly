package msgindex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix, dir
}

func TestSearchFoldsCaseYoAndMatchesPrefixes(t *testing.T) {
	ix, _ := open(t)
	ix.Put(
		Doc{Conv: "a", Entry: "1", At: 10, Role: RoleUser, Text: "Почему ЁЖИК не запускается в докере?"},
		Doc{Conv: "b", Entry: "2", At: 20, Role: RoleAgent, Text: "Restarting the Docker daemon fixed it."},
	)
	for q, want := range map[string]string{"ежик": "a", "ЕЖ": "a", "запуск": "a", "docker": "b", "dock restart": "b", "докер": "a"} {
		hits := ix.Search(q, Options{})
		if len(hits) != 1 || hits[0].Conv != want {
			t.Errorf("%q: %+v, want one hit in %s", q, hits, want)
		}
	}
	if hits := ix.Search("ежик docker", Options{}); len(hits) != 0 {
		t.Errorf("words of different messages matched: %+v", hits)
	}
	if hits := ix.Search("", Options{}); len(hits) != 0 {
		t.Errorf("empty query: %+v", hits)
	}
}

func TestSearchOrdersNewestFirstAndCapsPerChat(t *testing.T) {
	ix, _ := open(t)
	for i := 0; i < 5; i++ {
		ix.Put(Doc{Conv: "a", Entry: fmt.Sprint("e", i), At: int64(i), Role: RoleUser, Text: "deploy step"})
	}
	ix.Put(Doc{Conv: "b", Entry: "x", At: 2, Role: RoleUser, Text: "deploy"})
	hits := ix.Search("deploy", Options{PerConv: 2})
	if len(hits) != 3 || hits[0].Entry != "e4" || hits[1].Entry != "e3" || hits[2].Conv != "b" {
		t.Fatalf("%+v", hits)
	}
}

func TestPutReplacesAndSkipsUnchanged(t *testing.T) {
	ix, dir := open(t)
	d := Doc{Conv: "a", Entry: "1", At: 1, Role: RoleAgent, Text: "first draft"}
	ix.Put(d)
	size := func() int64 { st, _ := os.Stat(filepath.Join(dir, "messages.jsonl")); return st.Size() }
	before := size()
	ix.Put(d)
	if size() != before {
		t.Error("an unchanged message was written again")
	}
	d.Text = "second draft"
	ix.Put(d)
	if got := ix.Search("first", Options{}); len(got) != 0 {
		t.Errorf("the replaced text is found: %+v", got)
	}
	if got := ix.Search("second", Options{}); len(got) != 1 || ix.Count() != 1 {
		t.Errorf("%+v, count %d", got, ix.Count())
	}
}

func TestReopenKeepsMessagesAndMarks(t *testing.T) {
	ix, dir := open(t)
	ix.Replace("a", 100, []Doc{{Conv: "a", Entry: "1", At: 1, Role: RoleUser, Text: "hello world"}})
	ix.Put(Doc{Conv: "b", Entry: "1", At: 2, Role: RoleUser, Text: "bye world"})
	ix.Remove("b")
	ix.Close()
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	hits := again.Search("world", Options{})
	if len(hits) != 1 || hits[0].Conv != "a" || hits[0].Snippet != "hello world" {
		t.Fatalf("%+v", hits)
	}
	if size, ok := again.Size("a"); !ok || size != 100 {
		t.Errorf("mark %d %v", size, ok)
	}
	if again.Read("b") {
		t.Error("a removed conversation is still read")
	}
}

func TestReplaceForgetsMessagesThatAreGone(t *testing.T) {
	ix, _ := open(t)
	ix.Replace("a", 1, []Doc{{Conv: "a", Entry: "1", Text: "alpha", Role: RoleUser}, {Conv: "a", Entry: "2", Text: "beta", Role: RoleUser}})
	ix.Replace("a", 2, []Doc{{Conv: "a", Entry: "2", Text: "beta", Role: RoleUser}})
	if len(ix.Search("alpha", Options{})) != 0 || len(ix.Search("beta", Options{})) != 1 {
		t.Fatal("the rewritten conversation is not what the last reading says")
	}
}

func TestCutLastLineIsDropped(t *testing.T) {
	ix, dir := open(t)
	ix.Put(Doc{Conv: "a", Entry: "1", At: 1, Role: RoleUser, Text: "kept"})
	ix.Close()
	f, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"k":"p","c":"a","e":"2","x":"cu`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	again.Put(Doc{Conv: "a", Entry: "3", At: 3, Role: RoleUser, Text: "after"})
	if len(again.Search("kept", Options{})) != 1 || len(again.Search("after", Options{})) != 1 || len(again.Search("cu", Options{})) != 0 {
		t.Fatal("a cut line broke the log")
	}
}

func TestCompactKeepsTheLiveMessages(t *testing.T) {
	ix, dir := open(t)
	for i := 0; i < 20; i++ {
		ix.Put(Doc{Conv: "a", Entry: "1", At: 1, Role: RoleAgent, Text: fmt.Sprint("version ", i)})
	}
	ix.Mark("a", 7)
	before, _ := os.Stat(filepath.Join(dir, "messages.jsonl"))
	if err := ix.Compact(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(dir, "messages.jsonl"))
	if after.Size() >= before.Size() {
		t.Error("compaction did not shrink the log")
	}
	if hits := ix.Search("version 19", Options{}); len(hits) != 1 {
		t.Errorf("%+v", hits)
	}
	ix.Put(Doc{Conv: "a", Entry: "2", At: 2, Role: RoleUser, Text: "still writing"})
	if len(ix.Search("writing", Options{})) != 1 {
		t.Error("the index does not take messages after a compaction")
	}
}

func TestMessagesAreInOrder(t *testing.T) {
	ix, _ := open(t)
	ix.Put(Doc{Conv: "a", Entry: "2", At: 2, Role: RoleAgent, Text: "answer"}, Doc{Conv: "a", Entry: "1", At: 1, Role: RoleUser, Text: "question"})
	got := ix.Messages("a")
	if len(got) != 2 || got[0].Text != "question" || got[1].Text != "answer" {
		t.Fatalf("%+v", got)
	}
}

func TestSnippetShowsTheMatchWithEllipses(t *testing.T) {
	text := strings.Repeat("слово ", 40) + "искомое тут " + strings.Repeat("конец ", 60)
	s := Snippet(text, []string{"иском"})
	if !strings.Contains(s, "искомое") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") || len([]rune(s)) > 200 {
		t.Errorf("%q", s)
	}
}

func TestNilIndexIsEmpty(t *testing.T) {
	var ix *Index
	ix.Put(Doc{Conv: "a", Entry: "1", Text: "x"})
	if ix.Search("x", Options{}) != nil || ix.Count() != 0 || ix.Messages("a") != nil {
		t.Fatal("a nil index found something")
	}
}

func BenchmarkSearch(b *testing.B) {
	ix, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	var docs []Doc
	for i := 0; i < 100000; i++ {
		docs = append(docs, Doc{Conv: fmt.Sprint("c", i%3000), Entry: fmt.Sprint("e", i), At: int64(i), Role: RoleUser,
			Text: fmt.Sprintf("сообщение номер %d про деплой сервиса %d и ошибку в базе %d", i, i%97, i%13)})
	}
	ix.Put(docs...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Search("деплой сервис", Options{})
	}
}
