package agenteval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// FakeImagegen stands in for `codex exec` when a stand's runtime draws a picture (RUNTIME_IMAGEGEN_BIN): it answers the
// events the picture service reads and leaves blank pictures where Codex leaves real ones, so an eval checks that the
// agent asked for a picture and the chat got it, without paying for a drawing.
func FakeImagegen(args []string, stdout io.Writer) error {
	brief := ""
	if len(args) > 0 {
		brief = args[len(args)-1]
	}
	n := 1
	if m := regexp.MustCompile(`(?i)\b([1-8])\s+(?:distinct\s+)?(?:pictures|images|variants)`).FindStringSubmatch(brief); m != nil {
		n, _ = strconv.Atoi(m[1])
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".codex")
	}
	thread := fmt.Sprintf("eval-%d", time.Now().UnixNano())
	dir := filepath.Join(home, "generated_images", thread)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	emit := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintln(stdout, string(b))
	}
	emit(map[string]any{"type": "thread.started", "thread_id": thread})
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0xee
	}
	img.Set(32, 32, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	for i := 1; i <= n; i++ {
		time.Sleep(300 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ig_%d.png", i)), buf.Bytes(), 0o600); err != nil {
			return err
		}
	}
	time.Sleep(500 * time.Millisecond)
	emit(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "Done."}})
	emit(map[string]any{"type": "turn.completed"})
	return nil
}
