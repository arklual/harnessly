package searchindex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/racetest"
)

func open(t *testing.T) *SQL {
	t.Helper()
	ix, db, err := OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return ix
}

func ids(h []Hit) string {
	s := ""
	for _, x := range h {
		s += x.ID + ","
	}
	return s
}

func TestSearchWordsPrefixAndYo(t *testing.T) {
	ctx := context.Background()
	ix := open(t)
	_ = ix.Put(ctx,
		Doc{Space: "s", Kind: KindDesign, ID: "p1", Parent: "p1", Title: "Лендинг для кофейни", Body: "Главная Меню", UpdatedAt: 1},
		Doc{Space: "s", Kind: KindDesign, ID: "p2", Parent: "p2", Title: "Отчёт за квартал", UpdatedAt: 2},
		Doc{Space: "s", Kind: KindApp, ID: "kanban", Title: "Kanban board", Body: "tasks", UpdatedAt: 3},
		Doc{Space: "other", Kind: KindDesign, ID: "p9", Title: "Лендинг чужой", UpdatedAt: 4},
	)
	for q, want := range map[string]string{"лендинг": "p1,", "кофе": "p1,", "отчет": "p2,", "ОТЧЁТ квар": "p2,", "kan": "kanban,", "меню лендинг": "p1,"} {
		h, err := ix.Search(ctx, Query{Space: "s", Text: q})
		if err != nil || ids(h) != want {
			t.Errorf("%q: %v %v, want %s", q, ids(h), err, want)
		}
	}
	if h, _ := ix.Search(ctx, Query{Space: "s", Text: "лендинг", Kinds: []string{KindApp}}); len(h) != 0 {
		t.Errorf("a kind filter: %v", h)
	}
	if _, err := ix.Search(ctx, Query{Space: "s", Text: " ,. "}); err != ErrEmpty {
		t.Errorf("empty: %v", err)
	}
	if h, _ := ix.Search(ctx, Query{Space: "s", Text: "100%"}); len(h) != 0 {
		t.Errorf("a percent sign is a character: %v", h)
	}
}

func TestAllowFillsThePage(t *testing.T) {
	ctx := context.Background()
	ix := open(t)
	for i := 0; i < 300; i++ {
		_ = ix.Put(ctx, Doc{Space: "s", Kind: KindDesign, ID: fmt.Sprintf("p%d", i), Parent: fmt.Sprintf("p%d", i), Title: "design", UpdatedAt: int64(1000 - i)})
	}
	// The 250 newest are closed to the person; the page still has its five.
	h, err := ix.Search(ctx, Query{Space: "s", Text: "design", Limit: 5, Allow: func(h Hit) bool { return h.UpdatedAt <= 750 }})
	if err != nil || len(h) != 5 {
		t.Fatalf("%d %v", len(h), err)
	}
}

func TestReplaceAndRemove(t *testing.T) {
	ctx := context.Background()
	ix := open(t)
	_ = ix.Replace(ctx, "s", KindApp, "", []Doc{{ID: "a", Title: "alpha"}, {ID: "b", Title: "beta"}})
	_ = ix.Replace(ctx, "s", KindApp, "", []Doc{{ID: "b", Title: "beta two"}})
	if h, _ := ix.Search(ctx, Query{Space: "s", Text: "alpha"}); len(h) != 0 {
		t.Errorf("a replaced-away app stays: %v", h)
	}
	if h, _ := ix.Search(ctx, Query{Space: "s", Text: "two"}); len(h) != 1 {
		t.Errorf("an updated one: %v", h)
	}
	_ = ix.Remove(ctx, "s", KindApp, "b")
	if n, _ := ix.Count(ctx, "s"); n != 0 {
		t.Errorf("count %d", n)
	}
}

// 10 000 documents: a question is answered in well under 400 ms.
func TestTenThousandUnder400ms(t *testing.T) {
	ctx := context.Background()
	ix := open(t)
	db := ix.DB.(dbQ).DB
	tx, _ := db.BeginTx(ctx, nil)
	st, _ := tx.PrepareContext(ctx, upsert)
	for i := 0; i < 10000; i++ {
		title := fmt.Sprintf("Проект %d лендинг номер %d", i, i*7)
		_, err := st.ExecContext(ctx, "s", KindDesign, fmt.Sprintf("p%d", i), fmt.Sprintf("p%d", i), title, "доска главная меню", Normalize(title+" доска главная меню"), int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	_ = tx.Commit()
	for _, q := range []string{"лендинг", "проект 9999", "zzzz", "меню главная лендинг"} {
		start := time.Now()
		h, err := ix.Search(ctx, Query{Space: "s", Text: q, Limit: 20, Allow: func(Hit) bool { return true }})
		took := time.Since(start)
		if err != nil || (took > 400*time.Millisecond && racetest.StrictTiming()) {
			t.Errorf("%q: %v in %v", q, err, took)
		}
		t.Logf("%q: %d hits in %v", q, len(h), took)
	}
}
