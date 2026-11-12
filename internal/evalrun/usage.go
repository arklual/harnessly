package evalrun

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// transcriptUse is what a Claude Code transcript says the session used: one model call per distinct assistant message id
// (a streamed message is written more than once; the last write is the whole one).
type transcriptUse struct {
	calls             int
	firstCtx, peakCtx int
	tokensIn          int64
	tokensOut         int64
}

type usageLine struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Usage struct {
			In     int64 `json:"input_tokens"`
			Out    int64 `json:"output_tokens"`
			Read   int64 `json:"cache_read_input_tokens"`
			Create int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readTranscript(path string) (transcriptUse, error) {
	f, err := os.Open(path)
	if err != nil {
		return transcriptUse{}, err
	}
	defer f.Close()
	type call struct{ ctx, out int64 }
	var order []string
	byID := map[string]call{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var l usageLine
		if json.Unmarshal([]byte(line), &l) != nil || l.Type != "assistant" || l.Message.ID == "" {
			continue
		}
		u := l.Message.Usage
		if _, seen := byID[l.Message.ID]; !seen {
			order = append(order, l.Message.ID)
		}
		byID[l.Message.ID] = call{ctx: u.In + u.Read + u.Create, out: u.Out}
	}
	var t transcriptUse
	for i, id := range order {
		c := byID[id]
		if i == 0 {
			t.firstCtx = int(c.ctx)
		}
		if int(c.ctx) > t.peakCtx {
			t.peakCtx = int(c.ctx)
		}
		t.tokensIn += c.ctx
		t.tokensOut += c.out
	}
	t.calls = len(order)
	return t, sc.Err()
}
