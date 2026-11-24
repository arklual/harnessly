// Package mediagen makes video and pictures with a media provider of the registry (internal/extproviders: Higgsfield): a
// job is a request to the provider that runs on for a while, so it has a queue (a few at once on a machine), a state, a
// poll of the provider's status, a cancel and a download of the result into the job's own folder (and the asker's, when
// one is named), beside an origin file that says where the clip came from (provider, model, prompt, source frames).
//
// Money is spent only here: before a request is sent the provider is asked its price (free), the price is on the job,
// and a job over its cap (Request.MaxUSD, the machine's daily cap) is refused, not sent.
//
// A job belongs to whoever asked: a chat (an agent's generate_video), a Sinopia design. imagegen is the same for
// Codex's pictures and stays as it is.
package mediagen

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

const (
	// MaxRunning is how many jobs a machine sends to providers at once; the rest wait in the queue.
	MaxRunning = 3
	// RunTimeout bounds one job from its start to its file.
	RunTimeout = 40 * time.Minute
	// DefaultMaxUSD is what one job may cost when the request names no cap; DefaultDailyUSD, what a machine's jobs may
	// cost in a day.
	DefaultMaxUSD   = 2.0
	DefaultDailyUSD = 10.0
	maxPrompt       = 5000
	maxFrames       = 2
	maxFrameBytes   = 20 << 20
	maxResultBytes  = 1 << 30
	keepJobs        = 300
	// SecretName is the secret of the machine's safe a Higgsfield key is asked for under when no provider holds one:
	// `harnessly secret ask higgsfield-key`.
	SecretName = "higgsfield-key"
)

// State is where a job is.
type State string

const (
	Queued   State = "queued"
	Running  State = "running"
	Done     State = "done"
	Failed   State = "failed"
	Canceled State = "canceled"
)

// Ended says whether the job is over.
func (s State) Ended() bool { return s == Done || s == Failed || s == Canceled }

// Request is what to make.
type Request struct {
	Kind extproviders.MediaKind `json:"kind"`
	// Provider is the account of the registry ("" for the default Higgsfield account that holds a key).
	Provider string `json:"provider,omitempty"`
	// Model is an endpoint ID, or a family (kling, seedance, ltx, soul); "" is the default of the kind.
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt"`
	// Frames are the pictures the request starts from: files (absolute paths) or https addresses. The first is the first
	// frame, the second the last one (a model that takes it).
	Frames     []string `json:"frames,omitempty"`
	Seconds    int      `json:"seconds,omitempty"`
	Ratio      string   `json:"ratio,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	// Audio asks for the sound of a clip; nil: without (it costs more and a clip from a scene is silent anyway).
	Audio *bool `json:"audio,omitempty"`
	// Voice is the voice of speech (a preset voice id of Higgsfield's MCP); "" for the model's default.
	Voice string `json:"voice,omitempty"`
	// Params are the model's own further settings, sent as they are (Higgsfield's MCP only: models_explore lists them).
	Params map[string]any `json:"params,omitempty"`
	// Allowance says what pays on Higgsfield's MCP: "trial" (the free-trial unlimited generations), "credits", or ""
	// when the person has not said (a job that needs the choice stops and asks).
	Allowance string `json:"allowance,omitempty"`
	// MaxUSD is the most the job may cost by the provider's price; 0 is DefaultMaxUSD.
	MaxUSD float64 `json:"maxUsd,omitempty"`
	// Conv is the chat that asked ("" for none); Owner says who else did (e.g. "design:<id>").
	Conv  string `json:"conv,omitempty"`
	Owner string `json:"owner,omitempty"`
	// CopyTo is a folder the result is also copied into, with its origin file ("" for none), as <Name>-<n>.<ext>.
	CopyTo string `json:"copyTo,omitempty"`
	Name   string `json:"name,omitempty"`
	// SaveOK says, right before the copy, whether CopyTo may receive it; a request with CopyTo and no SaveOK copies nothing.
	SaveOK func(real string) bool `json:"-"`
	// FrameOK says, when the job starts, whether a frame file may be read; a request with a frame file and no FrameOK is
	// refused.
	FrameOK func(string) bool `json:"-"`
	// Origin is what the asker adds to the origin file (the scene, the camera, the outputs the frames came from).
	Origin map[string]any `json:"origin,omitempty"`
}

// File is one result of a job.
type File struct {
	N    int    `json:"n"`
	Path string `json:"path"`
	// Copy is the file in the asker's folder ("" when none was asked or the copy failed).
	Copy string `json:"copy,omitempty"`
	// OriginFile is the origin file beside Copy (or Path).
	OriginFile string `json:"originFile,omitempty"`
}

// Origin is what is written beside a result: where it came from.
type Origin struct {
	Provider  string                 `json:"provider"`
	Account   string                 `json:"account,omitempty"`
	Model     string                 `json:"model"`
	Endpoint  string                 `json:"endpoint"`
	Kind      extproviders.MediaKind `json:"kind"`
	Prompt    string                 `json:"prompt"`
	Sources   []string               `json:"sources,omitempty"`
	RequestID string                 `json:"requestId,omitempty"`
	CostUSD   float64                `json:"costUsd,omitempty"`
	Credits   float64                `json:"credits,omitempty"`
	Seconds   int                    `json:"seconds,omitempty"`
	Ratio     string                 `json:"ratio,omitempty"`
	CreatedAt string                 `json:"createdAt"`
	Extra     map[string]any         `json:"extra,omitempty"`
}

// Snapshot is a job as it is now.
type Snapshot struct {
	ID       string                 `json:"id"`
	Kind     extproviders.MediaKind `json:"kind"`
	Provider string                 `json:"provider"`
	Model    string                 `json:"model"`
	Endpoint string                 `json:"endpoint"`
	Prompt   string                 `json:"prompt"`
	Frames   []string               `json:"frames,omitempty"`
	Seconds  int                    `json:"seconds,omitempty"`
	Conv     string                 `json:"conv,omitempty"`
	Owner    string                 `json:"owner,omitempty"`
	State    State                  `json:"state"`
	// Status is the provider's own word for where the request is (queued, in_progress, …), for the card.
	Status string `json:"status,omitempty"`
	// EstimateUSD is the provider's price of the request (0 until it is asked); MaxUSD is the cap the job runs under.
	EstimateUSD float64 `json:"estimateUsd"`
	// Credits is the price in the provider's credits, when it counts in them (Higgsfield's MCP).
	Credits   float64 `json:"credits,omitempty"`
	MaxUSD    float64 `json:"maxUsd"`
	RequestID string  `json:"requestId,omitempty"`
	Files     []File  `json:"files"`
	Error     string  `json:"error,omitempty"`
	// Meta is the line the card shows under the clip: the model and the price.
	Meta      string         `json:"meta,omitempty"`
	Origin    map[string]any `json:"origin,omitempty"`
	StartedAt int64          `json:"startedAt"`
	EndedAt   int64          `json:"endedAt,omitempty"`
}

// Registry is the part of the providers' registry the jobs use.
type Registry interface {
	List() ([]extproviders.View, error)
	Get(id string) (extproviders.Provider, error)
	Key(p extproviders.Provider) (string, error)
}

// Secrets is the part of the safe the jobs use to find a key that no provider holds.
type Secrets interface {
	Has(name string) bool
	Reveal(name string) (string, error)
}

// Service runs and remembers the jobs.
type Service struct {
	// Dir is where the jobs are kept: <Dir>/<job id>/job.json and the results.
	Dir      string
	Registry Registry
	Secrets  Secrets
	// Client sends the requests to the provider (nil: a client that follows no redirect, with a timeout); Download
	// fetches results from the provider's CDN (nil: a client with a long timeout).
	Client   *http.Client
	Download *http.Client
	// AllowHTTP lets a result or a status address be http (a test's server); a provider's address is the registry's.
	AllowHTTP bool
	// MCP is the person's Higgsfield MCP connection (nil: none): it serves when no key does, and alone makes audio.
	MCP MCPConn
	// Changed is told a job's new state; nil for nothing.
	Changed func(Snapshot)
	// DailyUSD is what the jobs of a day may cost together (0: DefaultDailyUSD).
	DailyUSD float64
	// PollFirst and PollMax are the first and the longest wait between two polls (0: 2s and 10s).
	PollFirst, PollMax time.Duration
	Now                func() time.Time

	mu     sync.Mutex
	jobs   map[string]*job
	order  []string
	loaded bool
	slots  chan struct{}
	// imageCatalog is the MCP's list of picture models as last read (images.go), at imageCatalogAt.
	imageCatalog   []ImageModel
	imageCatalogAt time.Time
}

type job struct {
	snap   Snapshot
	req    Request
	tg     target
	done   chan struct{}
	cancel context.CancelFunc
}

// Errors a caller tells apart.
var (
	// ErrBadRequest is a request that cannot be made.
	ErrBadRequest = errors.New("bad request")
	// ErrNoKey: no media provider holds a key.
	ErrNoKey = errors.New("no key")
	// ErrNoMCP: audio, or a request that names the MCP connection, without it.
	ErrNoMCP = errors.New("no Higgsfield MCP connection")
	// ErrOverCap: the price is over the cap.
	ErrOverCap = errors.New("over the cap")
)

// NoMCPMessage is what a caller is told when the Higgsfield MCP connection is missing.
const NoMCPMessage = "Higgsfield's MCP is not connected (it makes music, speech and sound, and pictures and clips without an API key). " +
	"The person connects «Higgsfield (MCP)» in Settings → Connections and signs in with their Higgsfield account; " +
	"generation needs a Higgsfield plan or its free trial (https://higgsfield.ai/mcp)."

// NoKeyMessage is what a caller is told when there is no key: how to connect Higgsfield, and the safe's way to give the key.
const NoKeyMessage = "Higgsfield is not connected: no API key and no «Higgsfield (MCP)» connection. The simplest way: the person connects " +
	"«Higgsfield (MCP)» in Settings → Connections (a Higgsfield plan or its free trial pays, no key). Or the person adds a key in Settings → Providers → Higgsfield " +
	"(key ID and secret as «id:secret», from console.higgsfield.ai), or you ask for it through the safe, outside the chat: " +
	"`harnessly secret ask " + SecretName + " --why \"Higgsfield API: video generation in harnessly\"` (the value is «id:secret»). " +
	"Never ask the person to paste the key into the chat."

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *Service) download() *http.Client {
	if s.Download != nil {
		return s.Download
	}
	return &http.Client{Timeout: 20 * time.Minute}
}

func (s *Service) dailyUSD() float64 {
	if s.DailyUSD > 0 {
		return s.DailyUSD
	}
	return DefaultDailyUSD
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeName(s string) string {
	s = strings.Trim(unsafeName.ReplaceAllString(strings.TrimSpace(s), "-"), "-.")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// target is the provider a request goes to: its address, key and the name it is called.
type target struct {
	base, key, name string
	// mcp: the person's Higgsfield MCP connection, not the REST API.
	mcp bool
}

// resolve finds the provider of a request: the account named, else the default Higgsfield account that holds a key,
// else the key under SecretName in the safe.
func (s *Service) resolve(id string, kind extproviders.MediaKind) (target, error) {
	mcp := target{name: "Higgsfield (MCP)", mcp: true}
	mcpOK := s.MCP != nil && s.MCP.Connected()
	if kind == extproviders.KindAudio || strings.EqualFold(id, MCPConnID) || strings.EqualFold(id, "mcp") {
		if !mcpOK {
			return target{}, ErrNoMCP
		}
		return mcp, nil
	}
	tg, err := s.resolveKey(id)
	if errors.Is(err, ErrNoKey) && (id == "" || strings.EqualFold(id, "higgsfield")) && mcpOK {
		return mcp, nil
	}
	return tg, err
}

// resolveKey finds a provider that holds a REST API key.
func (s *Service) resolveKey(id string) (target, error) {
	if s.Registry != nil {
		views, err := s.Registry.List()
		if err != nil {
			return target{}, err
		}
		var pick *extproviders.View
		for i, v := range views {
			if v.API != extproviders.APIHiggsfield || !v.HasKey {
				continue
			}
			if id != "" {
				if v.ID == id || strings.EqualFold(v.Name, id) {
					pick = &views[i]
					break
				}
				continue
			}
			if pick == nil || v.Default {
				pick = &views[i]
			}
		}
		if pick != nil {
			p, err := s.Registry.Get(pick.ID)
			if err != nil {
				return target{}, err
			}
			key, err := s.Registry.Key(p)
			if err != nil || key == "" {
				return target{}, ErrNoKey
			}
			return target{base: p.BaseURL, key: key, name: p.Name}, nil
		}
	}
	if (id == "" || strings.EqualFold(id, "higgsfield")) && s.Secrets != nil && s.Secrets.Has(SecretName) {
		key, err := s.Secrets.Reveal(SecretName)
		if err == nil && extproviders.ValidMediaKey(strings.TrimSpace(key)) {
			return target{base: extproviders.HiggsfieldBase, key: strings.TrimSpace(key), name: "Higgsfield"}, nil
		}
	}
	return target{}, ErrNoKey
}

// plan is a request checked and turned into the provider's endpoint and body.
type plan struct {
	req      Request
	model    extproviders.MediaModel
	endpoint string
	body     map[string]any
}

// Normalize checks and fills a request.
func Normalize(r Request) (Request, error) {
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Kind == "" {
		r.Kind = extproviders.KindVideo
	}
	if r.Kind != extproviders.KindVideo && r.Kind != extproviders.KindImage && r.Kind != extproviders.KindAudio {
		return r, fmt.Errorf("%w: kind is video, image or audio", ErrBadRequest)
	}
	switch r.Allowance {
	case "", "trial", "credits":
	default:
		return r, fmt.Errorf("%w: allowance is trial or credits", ErrBadRequest)
	}
	if len(r.Prompt) > maxPrompt {
		return r, fmt.Errorf("%w: the prompt is longer than %d characters", ErrBadRequest, maxPrompt)
	}
	if len(r.Frames) > maxFrames {
		return r, fmt.Errorf("%w: at most %d frames (the first and the last)", ErrBadRequest, maxFrames)
	}
	if r.Seconds < 0 || r.Seconds > 360 || (r.Seconds > 60 && r.Kind != extproviders.KindAudio) {
		return r, fmt.Errorf("%w: seconds is 1–60 (1–360 for audio)", ErrBadRequest)
	}
	if r.MaxUSD < 0 {
		return r, fmt.Errorf("%w: max_usd is not negative", ErrBadRequest)
	}
	if r.MaxUSD == 0 {
		r.MaxUSD = DefaultMaxUSD
	}
	r.Name = safeName(r.Name)
	for i, f := range r.Frames {
		f = strings.TrimSpace(f)
		if f == "" {
			return r, fmt.Errorf("%w: an empty frame", ErrBadRequest)
		}
		r.Frames[i] = f
	}
	return r, nil
}

func isURL(s string) bool { return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") }

var frameExt = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}

// makePlan turns a request into the provider's endpoint and body, by the model's own limits. Frames that are files are
// not read here: the body holds their place (frameRef) and the job uploads them before it sends.
func makePlan(r Request) (plan, error) {
	fromImage := len(r.Frames) > 0
	name := r.Model
	if name == "" {
		switch {
		case r.Kind == extproviders.KindImage:
			name = extproviders.DefaultImageModel
		case fromImage:
			name = extproviders.DefaultVideoModel
		default:
			name = extproviders.DefaultTextVideo
		}
	}
	m, known := extproviders.FindMediaModel(name, r.Kind, fromImage)
	if !known {
		// A model of the provider's 80 that the catalog does not list: it takes the common fields, by its ID.
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*){1,4}$`).MatchString(name) {
			return plan{}, fmt.Errorf("%w: no model %q (use kling, seedance, ltx, soul, or an endpoint ID like kling-video/v3.0/std/image-to-video)", ErrBadRequest, name)
		}
		m = extproviders.MediaModel{ID: name, Label: name, Kind: r.Kind, FromImage: fromImage, LastFrame: len(r.Frames) > 1}
	}
	if m.Kind != r.Kind {
		return plan{}, fmt.Errorf("%w: %s makes %ss, not %ss", ErrBadRequest, m.ID, m.Kind, r.Kind)
	}
	if m.FromImage && !fromImage {
		return plan{}, fmt.Errorf("%w: %s starts from a picture: give frames", ErrBadRequest, m.ID)
	}
	if !m.FromImage && fromImage && r.Kind == extproviders.KindVideo && known {
		return plan{}, fmt.Errorf("%w: %s takes no picture: use an image-to-video model or drop frames", ErrBadRequest, m.ID)
	}
	if r.Kind == extproviders.KindImage && fromImage {
		return plan{}, fmt.Errorf("%w: pictures are made from the prompt only", ErrBadRequest)
	}
	if len(r.Frames) > 1 && !m.LastFrame {
		return plan{}, fmt.Errorf("%w: %s takes the first frame only", ErrBadRequest, m.ID)
	}
	if r.Prompt == "" && known && !(m.Family == "seedance" && m.FromImage) {
		return plan{}, fmt.Errorf("%w: the prompt is empty", ErrBadRequest)
	}
	body := map[string]any{}
	if r.Prompt != "" {
		body["prompt"] = r.Prompt
	}
	if len(r.Frames) > 0 {
		body["image_url"] = frameRef(0)
	}
	if len(r.Frames) > 1 {
		body["end_image_url"] = frameRef(1)
	}
	if r.Kind == extproviders.KindVideo {
		sec := r.Seconds
		switch {
		case len(m.OnlySec) > 0:
			if sec == 0 {
				sec = m.OnlySec[0]
			}
			if !slices.Contains(m.OnlySec, sec) {
				return plan{}, fmt.Errorf("%w: %s takes %v seconds", ErrBadRequest, m.ID, m.OnlySec)
			}
		case m.MaxSec > 0:
			if sec == 0 {
				sec = max(m.MinSec, min(5, m.MaxSec))
			}
			if sec < m.MinSec || sec > m.MaxSec {
				return plan{}, fmt.Errorf("%w: %s takes %d–%d seconds", ErrBadRequest, m.ID, m.MinSec, m.MaxSec)
			}
		}
		if sec > 0 {
			body["duration"] = sec
			r.Seconds = sec
		}
		switch m.Audio {
		case "sound":
			body["sound"] = map[bool]string{true: "on", false: "off"}[r.Audio != nil && *r.Audio]
		case "generate_audio":
			body["generate_audio"] = r.Audio != nil && *r.Audio
		}
	}
	if r.Ratio != "" {
		if len(m.Ratios) > 0 && !slices.Contains(m.Ratios, r.Ratio) {
			return plan{}, fmt.Errorf("%w: %s takes the ratios %v", ErrBadRequest, m.ID, m.Ratios)
		}
		if len(m.Ratios) == 0 && known && fromImage {
			// The frame follows the image.
			return plan{}, fmt.Errorf("%w: %s follows the frame's own proportions: leave the ratio out", ErrBadRequest, m.ID)
		}
		body["aspect_ratio"] = r.Ratio
	}
	if r.Resolution != "" {
		if len(m.Resolutions) > 0 && !slices.Contains(m.Resolutions, r.Resolution) {
			return plan{}, fmt.Errorf("%w: %s takes the resolutions %v", ErrBadRequest, m.ID, m.Resolutions)
		}
		body["resolution"] = r.Resolution
	}
	return plan{req: r, model: m, endpoint: m.ID, body: body}, nil
}

// frameRef marks the place of a frame in a body, to be replaced by its address once it is uploaded.
type frameRef int

// Start begins a job and gives its first snapshot. Nothing is sent to the provider before the job's own goroutine has
// its price and a slot.
func (s *Service) Start(r Request) (Snapshot, error) {
	r, err := Normalize(r)
	if err != nil {
		return Snapshot{}, err
	}
	tg, err := s.resolve(r.Provider, r.Kind)
	if err != nil {
		return Snapshot{}, err
	}
	pl, err := planFor(r, tg)
	if err != nil {
		return Snapshot{}, err
	}
	for _, f := range pl.req.Frames {
		if isURL(f) {
			if !strings.HasPrefix(f, "https://") && !s.AllowHTTP {
				return Snapshot{}, fmt.Errorf("%w: a frame address is https", ErrBadRequest)
			}
			continue
		}
		if !filepath.IsAbs(f) {
			return Snapshot{}, fmt.Errorf("%w: a frame is an absolute path or an https address", ErrBadRequest)
		}
		if pl.req.FrameOK == nil || !pl.req.FrameOK(f) {
			return Snapshot{}, fmt.Errorf("%w: the frame %s may not be read", ErrBadRequest, f)
		}
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := frameExt[ext]; !ok {
			return Snapshot{}, fmt.Errorf("%w: a frame is png, jpg, webp or gif", ErrBadRequest)
		}
		st, err := os.Stat(f)
		if err != nil || !st.Mode().IsRegular() {
			return Snapshot{}, fmt.Errorf("%w: no such frame file %s", ErrBadRequest, f)
		}
		if st.Size() > maxFrameBytes {
			return Snapshot{}, fmt.Errorf("%w: the frame %s is over %d MB", ErrBadRequest, f, maxFrameBytes>>20)
		}
	}
	id := newID()
	ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	j := &job{done: make(chan struct{}), cancel: cancel, req: pl.req, tg: tg, snap: Snapshot{
		ID: id, Kind: pl.req.Kind, Provider: tg.name, Model: pl.model.Label, Endpoint: pl.endpoint, Prompt: pl.req.Prompt,
		Frames: slices.Clone(pl.req.Frames), Seconds: pl.req.Seconds, Conv: pl.req.Conv, Owner: pl.req.Owner, State: Queued,
		MaxUSD: pl.req.MaxUSD, Files: []File{}, Origin: pl.req.Origin, StartedAt: s.now().UnixMilli(),
	}}
	s.mu.Lock()
	s.loadLocked()
	if s.slots == nil {
		s.slots = make(chan struct{}, MaxRunning)
	}
	s.jobs[id] = j
	s.order = append(s.order, id)
	s.forgetLocked()
	snap := cloneSnap(j.snap)
	s.mu.Unlock()
	s.tell(snap)
	go s.run(ctx, j, pl, tg)
	return snap, nil
}

// Estimate asks the provider the price of a request: in USD, and in credits when the provider counts in them (0
// otherwise). It sends nothing to be made.
func (s *Service) Estimate(ctx context.Context, r Request) (Price, error) {
	r, err := Normalize(r)
	if err != nil {
		return Price{}, err
	}
	tg, err := s.resolve(r.Provider, r.Kind)
	if err != nil {
		return Price{}, err
	}
	pl, err := planFor(r, tg)
	if err != nil {
		return Price{}, err
	}
	b, err := s.backendFor(tg)
	if err != nil {
		return Price{}, err
	}
	usd, credits, err := b.estimate(ctx, pl)
	return Price{USD: usd, Credits: credits, Model: pl.model.Label, Via: tg.name}, err
}

// Price is what a request would cost.
type Price struct {
	USD, Credits float64
	Model, Via   string
}

// planFor checks a request for the way it goes by.
func planFor(r Request, tg target) (plan, error) {
	if tg.mcp {
		return makeMCPPlan(r)
	}
	if r.Kind == extproviders.KindAudio {
		return plan{}, fmt.Errorf("%w: audio is made by the Higgsfield MCP connection only", ErrBadRequest)
	}
	if len(r.Params) > 0 || r.Voice != "" || r.Allowance != "" {
		return plan{}, fmt.Errorf("%w: params, voice and allowance are of the Higgsfield MCP connection, not of an API key", ErrBadRequest)
	}
	return makePlan(r)
}

func fillFrames(body map[string]any, addr func(i int) string) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		if ref, ok := v.(frameRef); ok {
			v = addr(int(ref))
		}
		out[k] = v
	}
	return out
}

// Get is a job's snapshot.
func (s *Service) Get(id string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	j, ok := s.jobs[id]
	if !ok {
		return Snapshot{}, false
	}
	return cloneSnap(j.snap), true
}

// List gives the jobs of a chat or an owner (either may be ""), oldest first.
func (s *Service) List(conv, owner string) []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	out := []Snapshot{}
	for _, id := range s.order {
		j := s.jobs[id]
		if (conv != "" && j.snap.Conv != conv) || (owner != "" && j.snap.Owner != owner) {
			continue
		}
		out = append(out, cloneSnap(j.snap))
	}
	return out
}

// Wait waits for a job to end, or for ctx; it gives the job as it is then.
func (s *Service) Wait(ctx context.Context, id string) (Snapshot, bool) {
	s.mu.Lock()
	s.loadLocked()
	j, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, false
	}
	select {
	case <-j.done:
	case <-ctx.Done():
	}
	return s.Get(id)
}

// Cancel stops a job: one in the queue at once, one at the provider while the provider has not begun it (it refunds
// what it reserved); one that is being made cannot be stopped, and the error says so.
func (s *Service) Cancel(ctx context.Context, id string) (Snapshot, error) {
	s.mu.Lock()
	s.loadLocked()
	j, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: no such job %s", ErrBadRequest, id)
	}
	snap, _ := s.Get(id)
	if snap.State.Ended() {
		return snap, nil
	}
	if snap.RequestID != "" {
		tg := j.tg
		if tg == (target{}) {
			var err error
			if tg, err = s.resolve(j.req.Provider, j.req.Kind); err != nil {
				return snap, err
			}
		}
		b, err := s.backendFor(tg)
		if err != nil {
			return snap, err
		}
		if err := b.cancel(ctx, snap.RequestID); err != nil {
			return snap, err
		}
	}
	// Canceled is written before the job's context is stopped: the job, seeing its context end, would otherwise
	// settle first as failed with "context canceled".
	s.settle(j, func(sn *Snapshot) {
		if !sn.State.Ended() {
			sn.State, sn.Error = Canceled, "canceled"
		}
	})
	j.cancel()
	<-j.done
	end, _ := s.Get(id)
	return end, nil
}

func (s *Service) forgetLocked() {
	for len(s.order) > keepJobs {
		id := s.order[0]
		if j := s.jobs[id]; j != nil && !j.snap.State.Ended() {
			break
		}
		s.order = s.order[1:]
		delete(s.jobs, id)
	}
}

func cloneSnap(s Snapshot) Snapshot {
	s.Files = slices.Clone(s.Files)
	if s.Files == nil {
		s.Files = []File{}
	}
	s.Frames = slices.Clone(s.Frames)
	return s
}

func (s *Service) tell(snap Snapshot) {
	if s.Changed != nil {
		s.Changed(snap)
	}
}

// settle changes a job under the lock, keeps it on disk and tells the watchers. A job that has ended stays as it ended.
func (s *Service) settle(j *job, f func(*Snapshot)) {
	s.mu.Lock()
	if j.snap.State.Ended() {
		s.mu.Unlock()
		return
	}
	f(&j.snap)
	j.snap.Meta = metaOf(j.snap)
	if j.snap.State.Ended() {
		j.snap.EndedAt = s.now().UnixMilli()
	}
	snap := cloneSnap(j.snap)
	s.mu.Unlock()
	s.save(snap)
	s.tell(snap)
}

// metaOf is the line the card shows: the model, the length and the price.
func metaOf(s Snapshot) string {
	parts := []string{s.Provider + " · " + s.Model}
	if s.Seconds > 0 {
		parts = append(parts, fmt.Sprintf("%d s", s.Seconds))
	}
	if s.Credits > 0 {
		parts = append(parts, fmt.Sprintf("%g credits", s.Credits))
	}
	if s.EstimateUSD > 0 {
		parts = append(parts, fmt.Sprintf("≈ $%.2f", s.EstimateUSD))
	}
	return strings.Join(parts, " · ")
}

// spentToday is what the jobs of the last day cost by their price: those the provider took.
func (s *Service) spentToday() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	since := s.now().Add(-24 * time.Hour).UnixMilli()
	total := 0.0
	for _, j := range s.jobs {
		if j.snap.StartedAt >= since && j.snap.RequestID != "" && j.snap.State != Canceled {
			total += j.snap.EstimateUSD
		}
	}
	return total
}

func (s *Service) fail(j *job, err error) {
	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		msg = "timed out waiting for the provider"
	}
	s.settle(j, func(sn *Snapshot) { sn.State, sn.Error = Failed, msg })
}

func (s *Service) run(ctx context.Context, j *job, pl plan, tg target) {
	defer close(j.done)
	defer j.cancel()
	c, err := s.backendFor(tg)
	if err != nil {
		s.fail(j, err)
		return
	}
	// The price comes first and costs nothing; a job over its cap is not sent.
	usd, credits, err := c.estimate(ctx, pl)
	if err != nil {
		s.fail(j, err)
		return
	}
	s.settle(j, func(sn *Snapshot) { sn.EstimateUSD, sn.Credits = usd, credits })
	if usd > pl.req.MaxUSD {
		s.fail(j, fmt.Errorf("%w: the provider's price is ≈ $%.2f, over the cap of $%.2f for this job. Nothing was sent. Tell the person the price and ask; run it again with a higher max_usd only if they agree", ErrOverCap, usd, pl.req.MaxUSD))
		return
	}
	if spent := s.spentToday(); spent+usd > s.dailyUSD() {
		s.fail(j, fmt.Errorf("%w: this machine's jobs of the last day cost ≈ $%.2f; with this one (≈ $%.2f) that is over the daily cap of $%.2f. Nothing was sent", ErrOverCap, spent, usd, s.dailyUSD()))
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		s.settle(j, func(sn *Snapshot) { sn.State, sn.Error = Canceled, "canceled" })
		return
	}
	if err := ctx.Err(); err != nil {
		s.settle(j, func(sn *Snapshot) { sn.State, sn.Error = Canceled, "canceled" })
		return
	}
	// Frames go up to the provider's storage and are named as the provider reads them.
	addrs := make([]string, len(pl.req.Frames))
	for i, f := range pl.req.Frames {
		if addrs[i], err = c.ref(ctx, f); err != nil {
			s.fail(j, fmt.Errorf("uploading the frame %s: %w", filepath.Base(f), err))
			return
		}
	}
	rid, err := c.submit(ctx, pl, addrs, j.snap.ID)
	if err != nil {
		s.fail(j, err)
		return
	}
	s.settle(j, func(sn *Snapshot) { sn.State, sn.RequestID, sn.Status = Running, rid, "queued" })
	st, err := s.poll(ctx, j, c, rid)
	if err != nil {
		// A job that was canceled has ended already: the failure is not told over it.
		s.fail(j, err)
		return
	}
	switch st.Status {
	case "completed":
		if err := s.collect(ctx, j, pl, tg, st); err != nil {
			s.fail(j, err)
		}
	case "nsfw":
		s.settle(j, func(sn *Snapshot) {
			sn.State, sn.Error = Failed, "the provider's moderation refused the frames or the result (nothing was charged)"
		})
	case "canceled":
		s.settle(j, func(sn *Snapshot) { sn.State, sn.Error = Canceled, "canceled" })
	default:
		why := st.Error
		if why == "" {
			why = "the provider failed to make it"
		}
		s.settle(j, func(sn *Snapshot) { sn.State, sn.Error = Failed, why+" (nothing was charged)" })
	}
}

// poll asks the provider for the request's status until it ends: after 2 s, then slower up to 10 s.
func (s *Service) poll(ctx context.Context, j *job, c backend, rid string) (hfStatus, error) {
	delay, max := s.PollFirst, s.PollMax
	if delay <= 0 {
		delay = 2 * time.Second
	}
	if max <= 0 {
		max = 10 * time.Second
	}
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return hfStatus{}, ctx.Err()
		case <-time.After(delay):
		}
		st, err := c.status(ctx, rid)
		switch {
		case err == nil:
			fails = 0
			switch st.Status {
			case "completed", "failed", "nsfw", "canceled":
				return st, nil
			}
			if st.Status != "" {
				s.settle(j, func(sn *Snapshot) { sn.Status = st.Status })
			}
		case errors.Is(err, errFatal):
			return hfStatus{}, err
		default:
			// A network failure or a 5xx: ask again, a few times.
			if fails++; fails >= 8 {
				return hfStatus{}, err
			}
		}
		delay = min(delay*3/2, max)
	}
}

// collect downloads what the provider made into the job's folder, writes the origin file and copies both where the asker said.
func (s *Service) collect(ctx context.Context, j *job, pl plan, tg target, st hfStatus) error {
	urls := st.urls()
	if len(urls) == 0 {
		return errors.New("the provider finished but gave no file")
	}
	dir := filepath.Join(s.Dir, j.snap.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var files []File
	for i, u := range urls {
		ext := extOf(u, pl.req.Kind)
		path := filepath.Join(dir, fmt.Sprintf("%s-%d%s", pl.req.Kind, i+1, ext))
		if err := s.fetch(ctx, u, path); err != nil {
			return fmt.Errorf("downloading the result: %w", err)
		}
		f := File{N: i + 1, Path: path}
		var err error
		org := Origin{Provider: "higgsfield", Account: tg.name, Credits: j.snap.Credits, Model: pl.model.Label, Endpoint: pl.endpoint, Kind: pl.req.Kind,
			Prompt: pl.req.Prompt, Sources: pl.req.Frames, RequestID: j.snap.RequestID, CostUSD: j.snap.EstimateUSD, Seconds: pl.req.Seconds,
			Ratio: pl.req.Ratio, CreatedAt: s.now().UTC().Format(time.RFC3339), Extra: pl.req.Origin}
		if f.OriginFile, err = writeOrigin(path, org); err != nil {
			return err
		}
		if pl.req.CopyTo != "" && pl.req.SaveOK != nil && pl.req.SaveOK(pl.req.CopyTo) {
			stem := pl.req.Name
			if stem == "" {
				stem = string(pl.req.Kind) + "-" + j.snap.ID
			}
			if err := os.MkdirAll(pl.req.CopyTo, 0o755); err == nil {
				cp := filepath.Join(pl.req.CopyTo, fmt.Sprintf("%s-%d%s", stem, i+1, ext))
				if copyFile(path, cp) == nil {
					f.Copy = cp
					if o, err := writeOrigin(cp, org); err == nil {
						f.OriginFile = o
					}
				}
			}
		}
		files = append(files, f)
	}
	s.settle(j, func(sn *Snapshot) { sn.State, sn.Files, sn.Status = Done, files, "completed" })
	return nil
}

func extOf(raw string, kind extproviders.MediaKind) string {
	if u, err := url.Parse(raw); err == nil {
		if e := strings.ToLower(filepath.Ext(u.Path)); regexp.MustCompile(`^\.[a-z0-9]{2,4}$`).MatchString(e) {
			return e
		}
	}
	if kind == extproviders.KindImage {
		return ".png"
	}
	if kind == extproviders.KindAudio {
		return ".mp3"
	}
	return ".mp4"
}

// writeOrigin writes <path>.origin.json and gives its path.
func writeOrigin(path string, o Origin) (string, error) {
	raw, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return "", err
	}
	out := path + ".origin.json"
	return out, os.WriteFile(out, append(raw, '\n'), 0o644)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

// fetch downloads a result address into path. The provider's key never goes there: the CDN is another host.
func (s *Service) fetch(ctx context.Context, raw, path string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && s.AllowHTTP)) || u.Host == "" {
		return errors.New("the result's address is not https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	res, err := s.download().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(res.Body, maxResultBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxResultBytes {
		err = errors.New("the result is over 1 GB")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
