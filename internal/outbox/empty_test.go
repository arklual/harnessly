package outbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// An empty box goes out as [] lists: the page refuses a conv snapshot whose queue items or messages are null.
func TestAnEmptyBoxCarriesEmptyLists(t *testing.T) {
	b := New("tab-1", wire.DefaultLimits, nil)
	raw, err := json.Marshal(struct {
		Q wire.Queue     `json:"q"`
		M []wire.Message `json:"m"`
	}{b.Queue(), b.Messages(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(raw); !strings.Contains(s, `"items":[]`) || !strings.Contains(s, `"m":[]`) {
		t.Fatalf("got %s", s)
	}
}
