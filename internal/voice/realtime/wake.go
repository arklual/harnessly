package realtime

// The wake phrase («Привет, harnessly»): the page listens for it itself, with a small speech model run in the browser
// (Vosk, WebAssembly) and a grammar of a few phrases. No sound leaves the page until the phrase is heard; then the page
// starts an ordinary call. Off by default. The machine keeps only the setting and a copy of the model: it fetches the
// model once from its publisher, checks its digest and repacks it as the tar.gz the browser engine reads.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Wake is the wake phrase's setting.
type Wake struct {
	On bool `json:"on"`
	// Silence ends a call the phrase started after this many seconds of nobody speaking; 0 is WakeSilenceDefault.
	Silence int `json:"silence,omitempty"`
}

// Bounds of Wake.Silence, seconds.
const (
	WakeSilenceDefault = 20
	WakeSilenceMin     = 5
	WakeSilenceMax     = 120
)

func (w *Wake) orDefault() Wake {
	out := Wake{Silence: WakeSilenceDefault}
	if w == nil {
		return out
	}
	out.On = w.On
	if w.Silence != 0 {
		out.Silence = min(max(w.Silence, WakeSilenceMin), WakeSilenceMax)
	}
	return out
}

// SaveWake writes the wake field alone: the other fields of the saved settings, known here or not, stay as they are.
func (s *Service) SaveWake(in Wake) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(s.settingsPath()); err == nil {
		_ = json.Unmarshal(raw, &doc)
	}
	w := in.orDefault()
	raw, _ := json.Marshal(w)
	doc["wake"] = raw
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	out, _ := json.Marshal(doc)
	tmp := s.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	if err := os.Rename(tmp, s.settingsPath()); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	return nil
}

// WakeModel is where the wake phrase's model comes from: the small Russian Vosk model (Apache-2.0, 46 MB zip).
var WakeModel = struct {
	URL    string
	SHA256 string
}{
	URL:    "https://alphacephei.com/vosk/models/vosk-model-small-ru-0.22.zip",
	SHA256: "961d5ff98a17f4aa6de69864d0aa71fa5bac682301d2b5d17a3f24c5c99a46d4",
}

const wakeModelFile = "wake-model.tar.gz"

func (s *Service) wakeModelPath() string { return filepath.Join(s.cfg.Dir, wakeModelFile) }

// serveWakeModel answers with the model's tar.gz, fetching it first when the machine has none yet. The browser engine
// keeps its own copy by the address, so a page asks for it once.
func (s *Service) serveWakeModel(w http.ResponseWriter, r *http.Request) {
	if err := s.ensureWakeModel(r); err != nil {
		answerErr(w, err)
		return
	}
	f, err := os.Open(s.wakeModelPath())
	if err != nil {
		answerErr(w, &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()})
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, wakeModelFile, st.ModTime(), f)
}

func (s *Service) ensureWakeModel(r *http.Request) error {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	if _, err := os.Stat(s.wakeModelPath()); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	zipPath := filepath.Join(s.cfg.Dir, "wake-model.zip.part")
	defer os.Remove(zipPath)
	if err := s.fetchWakeZip(r, zipPath); err != nil {
		return err
	}
	tmp := s.wakeModelPath() + ".part"
	if err := repackZip(zipPath, tmp); err != nil {
		_ = os.Remove(tmp)
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	if err := os.Rename(tmp, s.wakeModelPath()); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	return nil
}

func (s *Service) fetchWakeZip(r *http.Request, to string) error {
	url := WakeModel.URL
	if s.cfg.WakeModelURL != "" {
		url = s.cfg.WakeModelURL
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: err.Error()}
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: res.Status}
	}
	f, err := os.Create(to)
	if err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 200<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: err.Error()}
	}
	want := WakeModel.SHA256
	if s.cfg.WakeModelSHA256 != "" {
		want = s.cfg.WakeModelSHA256
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: "model digest " + got}
	}
	return nil
}

// repackZip writes the zip's files as a tar.gz with the same paths (one top folder, which the engine strips).
func repackZip(from, to string) error {
	zr, err := zip.OpenReader(from)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if strings.HasPrefix(name, "..") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("bad path in model: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				return err
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(f.UncompressedSize64)}); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return out.Close()
}
