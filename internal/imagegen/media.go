package imagegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	_ "image/jpeg"
	_ "image/png"
)

// Media draws pictures with a media provider of the person's (Higgsfield: their plan's credits), one picture a call. It
// is the same service the agents' generate_images uses with a provider, so its price checks and daily cap hold here too.
type Media interface {
	// Available says whether the provider is connected on this machine.
	Available() bool
	// Draw makes one picture and gives the path of its file.
	Draw(ctx context.Context, d MediaDraw) (string, error)
}

// MediaDraw is one picture asked of a media provider.
type MediaDraw struct {
	// Model is the provider's model ("" for its default).
	Model  string
	Prompt string
	// Ratio is the frame, "1:1", "16:9", … ("" for the model's own).
	Ratio string
	// Reference is a picture file the model starts from ("" for none): the picture being changed, the marks on it.
	Reference string
}

// modelID is the shape of a model id of the provider's catalog.
var modelID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,80}$`)

// ErrNoMedia: a request for Higgsfield on a machine where it is not connected.
var ErrNoMedia = errors.New("Higgsfield is not connected: the person connects «Higgsfield (MCP)» in Settings → Connections")

// mediaRatios are the frames every picture model of the provider takes.
var mediaRatios = [][2]int{{1, 1}, {4, 3}, {3, 4}, {16, 9}, {9, 16}, {3, 2}, {2, 3}}

func shapeRatio(s Shape) string {
	switch s {
	case Landscape:
		return "16:9"
	case Portrait:
		return "9:16"
	case Square:
		return "1:1"
	}
	return ""
}

// nearestRatio is the frame of mediaRatios closest to a picture's own, so that a changed picture keeps its shape.
func nearestRatio(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	want := math.Log(float64(w) / float64(h))
	best, dist := "", math.MaxFloat64
	for _, r := range mediaRatios {
		if d := math.Abs(math.Log(float64(r[0])/float64(r[1])) - want); d < dist {
			best, dist = fmt.Sprintf("%d:%d", r[0], r[1]), d
		}
	}
	return best
}

// mediaPrompt is what a media model is told: a picture model takes plain words, not the instructions about tools that
// Codex is given (Brief, editBrief).
func mediaPrompt(r Request) string {
	if r.Source == "" {
		return r.Prompt
	}
	var b strings.Builder
	switch r.Mode {
	case ModeMark:
		b.WriteString("The reference picture has coloured marks drawn over it: they are notes, not part of the picture. Do what they " +
			"point at and leave no mark in the result. ")
	case ModeNote:
		b.WriteString("A thin bright magenta rectangle on the reference picture frames an area: change only what is inside it and " +
			"leave no rectangle in the result. ")
	case ModeErase:
		b.WriteString("Bright magenta paint on the reference picture covers what is to be removed: remove whatever it touches, " +
			"entire objects, fill the place naturally, and leave no paint in the result. ")
	case ModeSize:
		f := formats[r.Format]
		fmt.Fprintf(&b, "Recompose the reference picture to the aspect ratio %d:%d: keep the main subject whole and unchanged, extend "+
			"the scene naturally where the new frame needs more of it. ", f[0], f[1])
	default:
		b.WriteString("Edit the reference picture. ")
	}
	b.WriteString("Keep everything not asked to change exactly as it is: subject, style, light, framing.")
	if r.Prompt != "" {
		b.WriteString(" Change: ")
		b.WriteString(r.Prompt)
	}
	return b.String()
}

// runMedia draws the job's pictures with the media provider, all at once, each published as soon as it is there.
func (s *Service) runMedia(ctx context.Context, j *job, r Request, work, out string) error {
	if s.Media == nil || !s.Media.Available() {
		return ErrNoMedia
	}
	d := MediaDraw{Model: r.Model, Prompt: mediaPrompt(r), Ratio: shapeRatio(r.Shape)}
	if r.Source != "" {
		ref, raw, err := stageSource(r, work)
		if err != nil {
			return err
		}
		d.Reference = ref
		d.Ratio = ""
		if r.Mode == ModeSize {
			f := formats[r.Format]
			d.Ratio = nearestRatio(f[0], f[1])
		} else if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil {
			d.Ratio = nearestRatio(cfg.Width, cfg.Height)
		}
	}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		made  int
		first error
	)
	for i := 0; i < r.Count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := s.Media.Draw(ctx, d)
			var raw []byte
			if err == nil {
				raw, err = readPicture(path)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			n := made + 1
			ext := pictureExt(raw)
			dst := filepath.Join(out, fmt.Sprintf("%d%s", n, ext))
			if err := replaceFile(dst, raw); err != nil {
				if first == nil {
					first = err
				}
				return
			}
			if s.addPicture(j, r, dst, n) {
				made++
			}
		}()
	}
	wg.Wait()
	if made < r.Count {
		if first == nil {
			first = errors.New("the provider gave no picture")
		}
		return first
	}
	return nil
}

// readPicture reads a picture the provider made, bounded, and checks it is one.
func readPicture(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("the provider's picture could not be read")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSource+1))
	if err != nil || len(raw) > maxSource {
		return nil, errors.New("the provider's picture could not be read or is too big")
	}
	// Go reads no webp: one is taken by its header (RIFF….WEBP), the others are decoded whole.
	if len(raw) > 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return raw, nil
	}
	if _, err := decodeBounded(raw, "the provider's picture"); err != nil {
		return nil, err
	}
	return raw, nil
}

// pictureExt names a picture's file by what its bytes are, not by the name the provider gave it.
func pictureExt(raw []byte) string {
	switch {
	case len(raw) > 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		return ".webp"
	case len(raw) > 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return ".jpg"
	}
	return ".png"
}
