package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func allowedURL(raw string) error {
	return agentbrowser.AllowedURL(raw)
}
func relativePath(cwd, path string) (string, string, error) {
	if cwd == "" || path == "" {
		return "", "", errors.New("A path inside the chat folder is required")
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", errors.New("Path refused: files must stay inside the chat folder, including symlinks")
	}
	return root, rel, nil
}
func insideFile(cwd, path string) (string, error) {
	root, rel, err := relativePath(cwd, path)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, rel)
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("Upload requires a regular file")
	}
	return full, nil
}
func saveImage(cwd, path string, data []byte) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	root, rel, err := relativePath(cwd, filepath.Dir(path))
	if err != nil {
		return err
	}
	// os.Root prevents a symlink swap between validation and creation from escaping the chat.
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := r.OpenFile(filepath.Join(rel, filepath.Base(path)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (t Tools) screenshot(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (*jsvalue.Object, error) {
	// Hidden widgets can leave a surface capture waiting indefinitely under Xvfb.
	capture, stop := context.WithTimeout(ctx, 1200*time.Millisecond)
	raw, err := i.AgentCall(capture, t.Caller, a.Tab, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 70})
	stop()
	activated := false
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		tabs := i.Tabs(t.Caller)
		window := 0
		for _, tab := range tabs {
			if tab.ID == a.Tab {
				window = tab.WindowID
			}
		}
		for _, tab := range tabs {
			if window != 0 && tab.WindowID == window && tab.Control == "person" {
				return nil, errors.New("Hidden screenshot stalled; the person controls this window. Wait or ask them to return control")
			}
		}
		if _, err = i.AgentCall(ctx, t.Caller, a.Tab, "Page.bringToFront", nil); err != nil {
			return nil, err
		}
		activated = true
		raw, err = i.AgentCall(ctx, t.Caller, a.Tab, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 70})
	}
	if err != nil {
		return nil, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err = json.Unmarshal(raw, &shot); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, err
	}
	data, err = capJPEG(data)
	if err != nil {
		return nil, err
	}
	shot.Data = base64.StdEncoding.EncodeToString(data)
	note := untrustedNote
	if activated {
		note += "\nActivated this tab because hidden screenshot capture stalled."
	}
	if a.Path != "" {
		b, e := base64.StdEncoding.DecodeString(shot.Data)
		if e != nil {
			return nil, e
		}
		if e = saveImage(t.Cwd, a.Path, b); e != nil {
			return nil, e
		}
		note += "\nScreenshot saved inside the chat folder."
	}
	return jsvalue.NewObject("content", []any{
		jsvalue.NewObject("type", "text", "text", st.redact(note)),
		jsvalue.NewObject("type", "image", "mimeType", "image/jpeg", "data", shot.Data),
	}), nil
}

// Resizing after capture avoids changing the page viewport for an image size limit.
func capJPEG(data []byte) ([]byte, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if max(cfg.Width, cfg.Height) <= 1568 {
		return data, nil
	}
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	scale := 1568 / float64(max(cfg.Width, cfg.Height))
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(cfg.Width)*scale), int(float64(cfg.Height)*scale)))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			dst.Set(x, y, src.At(int(float64(x)/scale), int(float64(y)/scale)))
		}
	}
	var out bytes.Buffer
	err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 70})
	return out.Bytes(), err
}
