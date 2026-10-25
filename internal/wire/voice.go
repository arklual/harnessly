package wire

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// ErrVoiceUnavailable is a machine that cannot recognize speech right now (no decoder, no recognition service); the
// page is told unsupported. Any other error of Server.Voice is a recording that failed: internal.
var ErrVoiceUnavailable = errors.New("voice: not available on this machine")

// VoiceBodyMax is the largest recording POST /api/v2/voice takes, in bytes: some ten minutes of speech in a compressed
// container.
const VoiceBodyMax = 20 << 20

// VoiceBudget is how long a recording may take to come and to be recognized.
const VoiceBudget = 2 * time.Minute

// VoiceLangs are the languages a recording may be recognized in; "" is ru.
var VoiceLangs = []string{"ru", "en", "auto"}

// Transcript is the answer of POST /api/v2/voice: what was said, empty for silence.
type Transcript struct {
	Text string `json:"text"`
}

func (Transcript) Validate() error { return nil }

// voice serves POST /api/v2/voice?lang=ru|en|auto: the body is a recording (webm/opus, mp4/aac, wav, whatever the
// decoder reads), the answer its text.
func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Voice == nil {
		refuse(w, RefuseNotFound)
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "ru"
	}
	ok := false
	for _, l := range VoiceLangs {
		ok = ok || l == lang
	}
	if !ok || r.ContentLength == 0 {
		refuse(w, RefuseBadRequest)
		return
	}
	if r.ContentLength > VoiceBodyMax {
		refuse(w, RefuseAttachmentTooLarge)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), VoiceBudget)
	defer cancel()
	body := &countingReader{r: http.MaxBytesReader(w, r.Body, VoiceBodyMax)}
	text, err := s.Voice(ctx, body, lang)
	switch {
	case err == nil && body.n == 0:
		refuse(w, RefuseBadRequest)
	case err == nil:
		s.writeChecked(w, r, Transcript{Text: text})
	case errors.Is(err, ErrVoiceUnavailable):
		refuse(w, RefuseUnsupported)
	case errors.Is(err, context.DeadlineExceeded):
		refuse(w, RefuseTimeout)
	default:
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuse(w, RefuseAttachmentTooLarge)
			return
		}
		agent.Log("warn", "v2: a recording was not recognized", agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}

// countingReader counts what the recording held, so an empty body is told from silence.
type countingReader struct {
	r interface{ Read([]byte) (int, error) }
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
