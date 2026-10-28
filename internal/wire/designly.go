package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Designly: the machine's design library over v2. A project is made from the library's template and a v2 conversation
// is opened in its folder; the library lists its projects with the conversations that work on them.

// DesignKind is what a project is for.
type DesignKind string

const (
	DesignSite   DesignKind = "site"
	DesignSlides DesignKind = "slides"
	DesignDS     DesignKind = "ds"
)

// DesignKinds is every DesignKind, in the schema's order.
var DesignKinds = []DesignKind{DesignSite, DesignSlides, DesignDS}

// The bounds of what a person writes for a new project.
const (
	DesignTitleMax  = 200
	DesignBriefMax  = 4000
	DesignSystemMax = 200
	DesignlyBodyMax = 64 << 10
	DesignBoardsMax = 200
)

// DesignDeckTone is how a deck's speaker notes are written.
type DesignDeckTone string

// DesignDeckTones is every DesignDeckTone, in the schema's order.
var DesignDeckTones = []DesignDeckTone{"talk", "biz", "short"}

// The bounds of a new deck's settings and of what a new project's first message carries.
const (
	DesignDeckMinutesMin = 1
	DesignDeckMinutesMax = 180
	DesignDeckSlidesMin  = 2
	DesignDeckSlidesMax  = 40
	DesignDeckLayoutsMax = 200
	DesignDeckVarsMax    = 32
	DesignDeckVarMax     = 64
	DesignAttachmentsMax = 20
)

// designDeckVar is the name of a deck's CSS variable a company template may set.
var designDeckVar = regexp.MustCompile(`^--deck-[a-z0-9-]{1,32}$`)

// DesignlyDeckStyle is the company template a new deck takes its look from: the file's name, the --deck-* variables
// read from it (POST /design/deck/pptx-style) and its count of slide layouts.
type DesignlyDeckStyle struct {
	Source  string            `json:"source"`
	Vars    map[string]string `json:"vars"`
	Layouts int               `json:"layouts"`
}

func (s DesignlyDeckStyle) Validate() error {
	errs := []error{need("source", s.Source)}
	if utf8.RuneCountInString(s.Source) > DesignSystemMax {
		errs = append(errs, errors.New("style.source: too long"))
	}
	if s.Layouts < 0 || s.Layouts > DesignDeckLayoutsMax {
		errs = append(errs, errors.New("style.layouts: out of range"))
	}
	if len(s.Vars) > DesignDeckVarsMax {
		errs = append(errs, errors.New("style.vars: too many"))
	}
	for k, v := range s.Vars {
		if !designDeckVar.MatchString(k) {
			errs = append(errs, errors.New("style.vars: not a --deck-* name"))
		}
		if v == "" || utf8.RuneCountInString(v) > DesignDeckVarMax {
			errs = append(errs, errors.New("style.vars: bad value"))
		}
	}
	return errors.Join(errs...)
}

// DesignlyDeck is what a new deck (kind slides) is asked to be: its length in minutes (0 or absent: the agent decides),
// its slide count (nil: the agent picks), the tone of the notes, whether the agent writes notes at all, and the company template's style.
type DesignlyDeck struct {
	Minutes int                `json:"minutes"`
	Slides  *int               `json:"slides"`
	Tone    DesignDeckTone     `json:"tone"`
	Script  bool               `json:"script"`
	Style   *DesignlyDeckStyle `json:"style"`
}

func (d DesignlyDeck) Validate() error {
	errs := []error{in("tone", d.Tone, DesignDeckTones)}
	if d.Minutes != 0 && (d.Minutes < DesignDeckMinutesMin || d.Minutes > DesignDeckMinutesMax) {
		errs = append(errs, errors.New("minutes: out of range"))
	}
	if d.Slides != nil && (*d.Slides < DesignDeckSlidesMin || *d.Slides > DesignDeckSlidesMax) {
		errs = append(errs, errors.New("slides: out of range"))
	}
	if d.Style != nil {
		errs = append(errs, d.Style.Validate())
	}
	return errors.Join(errs...)
}

// DesignlyCreate is the body of POST /api/v2/designly/create. Model nil: the agent's default; DesignSystem nil: the
// library's own. Deck is there only for the kind slides, and only from a page that knows decks. AttachmentIDs are
// uploads (POST /api/v2/upload) the first message carries: the brief's files, a company template.
type DesignlyCreate struct {
	Title         string        `json:"title"`
	Brief         string        `json:"brief"`
	Kind          DesignKind    `json:"kind"`
	Provider      Provider      `json:"provider"`
	Model         *string       `json:"model"`
	Effort        *Effort       `json:"effort"`
	DesignSystem  *string       `json:"designSystem"`
	Deck          *DesignlyDeck `json:"deck,omitempty"`
	AttachmentIDs []string      `json:"attachmentIds,omitempty"`
}

func (c DesignlyCreate) Validate() error {
	errs := []error{need("title", c.Title), need("brief", c.Brief), in("kind", c.Kind, DesignKinds), in("provider", c.Provider, Providers),
		needPtr("model", c.Model), needPtr("designSystem", c.DesignSystem)}
	if c.Effort != nil {
		errs = append(errs, in("effort", *c.Effort, Efforts))
	}
	if utf8.RuneCountInString(c.Title) > DesignTitleMax {
		errs = append(errs, errors.New("title: too long"))
	}
	if utf8.RuneCountInString(c.Brief) > DesignBriefMax {
		errs = append(errs, errors.New("brief: too long"))
	}
	if c.DesignSystem != nil && utf8.RuneCountInString(*c.DesignSystem) > DesignSystemMax {
		errs = append(errs, errors.New("designSystem: too long"))
	}
	if c.Deck != nil {
		if c.Kind != DesignSlides {
			errs = append(errs, errors.New("deck: only for kind slides"))
		}
		errs = append(errs, c.Deck.Validate())
	}
	if len(c.AttachmentIDs) > DesignAttachmentsMax {
		errs = append(errs, errors.New("attachmentIds: too many"))
	}
	for i, id := range c.AttachmentIDs {
		errs = append(errs, need(fmt.Sprintf("attachmentIds[%d]", i), id))
	}
	return errors.Join(errs...)
}

// DesignlyCreated is the answer of POST /api/v2/designly/create: the project and the conversation opened in it.
type DesignlyCreated struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Conv  string `json:"conv"`
}

func (c DesignlyCreated) Validate() error {
	errs := []error{need("id", c.ID), need("title", c.Title)}
	if !ValidConvID(c.Conv) {
		errs = append(errs, errors.New("conv: not a conversation id"))
	}
	return errors.Join(errs...)
}

// DesignBoard is a board of a project: Path is under the project's folder.
type DesignBoard struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

func (b DesignBoard) Validate() error {
	return errors.Join(need("title", b.Title), need("path", b.Path))
}

// DesignOwner is who made a project: the person's name, and whether it is the person asking.
type DesignOwner struct {
	Name string `json:"name"`
	Me   bool   `json:"me"`
}

func (o DesignOwner) Validate() error { return need("name", o.Name) }

// DesignArtifact is a project of the library. Convs are the v2 conversations whose folder is the project's. TouchedBy are
// the other conversations that worked on it, from any folder: their agent wrote its files or named it to the design CLI
// (read from their transcripts, so it holds for the chats from before), the latest activity first; absent from an
// older runtime. Owner is there for a project made after owners were recorded; an older one has none, and none is
// guessed.
type DesignArtifact struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	UpdatedAt   Ms            `json:"updatedAt"`
	Kind        DesignKind    `json:"kind"`
	Pinned      bool          `json:"pinned"`
	Cover       *string       `json:"cover"`
	ProjectPath string        `json:"projectPath"`
	Boards      []DesignBoard `json:"boards"`
	Convs       []string      `json:"convs"`
	TouchedBy   []string      `json:"touchedBy,omitempty"`
	Owner       *DesignOwner  `json:"owner,omitempty"`
	// Role is what the person may do in the project; absent from a runtime that does not restrict access.
	Role DesignRole `json:"role,omitempty"`
}

// DesignRole is a person's role in a project, weakest first.
type DesignRole string

// DesignRoles is every DesignRole, in the schema's order.
var DesignRoles = []DesignRole{"view", "comment", "edit", "manage", "owner"}

func (a DesignArtifact) Validate() error {
	errs := []error{need("id", a.ID), need("title", a.Title), nonNegative("updatedAt", int64(a.UpdatedAt)), in("kind", a.Kind, DesignKinds),
		needPtr("cover", a.Cover), each("boards", a.Boards)}
	if len(a.Boards) > DesignBoardsMax {
		errs = append(errs, errors.New("boards: too many"))
	}
	if a.Owner != nil {
		errs = append(errs, a.Owner.Validate())
	}
	if a.Role != "" {
		errs = append(errs, in("role", a.Role, DesignRoles))
	}
	for _, c := range a.Convs {
		if !ValidConvID(c) {
			errs = append(errs, errors.New("convs: not a conversation id"))
		}
	}
	for _, c := range a.TouchedBy {
		if !ValidConvID(c) {
			errs = append(errs, errors.New("touchedBy: not a conversation id"))
		}
	}
	return errors.Join(errs...)
}

// DesignLibrary is the answer of GET /api/v2/designly/library: the library server's port on the machine (the boards are
// served there), the library's folder, and the projects.
type DesignLibrary struct {
	Port      int              `json:"port"`
	Root      string           `json:"root"`
	Artifacts []DesignArtifact `json:"artifacts"`
}

func (l DesignLibrary) Validate() error {
	errs := []error{nonNegative("port", int64(l.Port)), need("root", l.Root), each("artifacts", l.Artifacts)}
	if l.Port > 65535 {
		errs = append(errs, errors.New("port: out of range"))
	}
	return errors.Join(errs...)
}

// DesignSystemInfo is a design system the machine has: what a new project may be built on.
type DesignSystemInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
}

func (d DesignSystemInfo) Validate() error {
	return errors.Join(need("id", d.ID), need("label", d.Label))
}

// DesignSystems is the answer of GET /api/v2/designly/systems.
type DesignSystems []DesignSystemInfo

func (d DesignSystems) Validate() error { return each("systems", d) }

// DesignlyPin is the body and the answer of POST /api/v2/designly/pin.
type DesignlyPin struct {
	ID     string `json:"id"`
	Pinned bool   `json:"pinned"`
}

func (p DesignlyPin) Validate() error { return need("id", p.ID) }

// DesignlyRefusal is the answer of a designly page that refuses: the code of the protocol, a message worded for the
// person, and the project that was made before the failure, if one was.
type DesignlyRefusal struct {
	R         ResultKind `json:"r"`
	Code      RefuseCode `json:"code"`
	Message   string     `json:"message"`
	ProjectID *string    `json:"projectId"`
}

func (d DesignlyRefusal) Validate() error {
	return errors.Join(in("r", d.R, []ResultKind{ResultRefused}), in("code", d.Code, RefuseCodes), need("message", d.Message), needPtr("projectId", d.ProjectID))
}

// DesignlyError is what a DesignlySource refuses with.
type DesignlyError struct {
	Code      RefuseCode
	Message   string
	ProjectID string
}

func (e *DesignlyError) Error() string { return e.Message }

// DesignlySource is the design library of the machine.
type DesignlySource interface {
	Create(ctx context.Context, c DesignlyCreate) (DesignlyCreated, error)
	Library(ctx context.Context) (DesignLibrary, error)
	Systems(ctx context.Context) ([]DesignSystemInfo, error)
	Pin(ctx context.Context, id string, pinned bool) error
}

// designlyBudget is how long a project may take to be made and its conversation to be opened.
const designlyBudget = 30 * time.Second

// designly serves /api/v2/designly/{create,library,systems,pin}.
func (s *Server) designly(w http.ResponseWriter, r *http.Request, what string) {
	if s.Designly == nil {
		refuse(w, RefuseNotFound)
		return
	}
	want := http.MethodGet
	if what == "create" || what == "pin" {
		want = http.MethodPost
	}
	if r.Method != want {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), designlyBudget)
	defer cancel()
	switch what {
	case "create":
		var body DesignlyCreate
		if !readDesignly(w, r, &body) {
			return
		}
		out, err := s.Designly.Create(ctx, body)
		if err != nil {
			s.designlyRefusal(w, r, err)
			return
		}
		s.writeChecked(w, r, out)
	case "library":
		out, err := s.Designly.Library(ctx)
		if err != nil {
			s.designlyRefusal(w, r, err)
			return
		}
		if out.Artifacts == nil {
			out.Artifacts = []DesignArtifact{}
		}
		for i := range out.Artifacts {
			if out.Artifacts[i].Boards == nil {
				out.Artifacts[i].Boards = []DesignBoard{}
			}
			if out.Artifacts[i].Convs == nil {
				out.Artifacts[i].Convs = []string{}
			}
		}
		s.writeChecked(w, r, out)
	case "systems":
		list, err := s.Designly.Systems(ctx)
		if err != nil {
			s.designlyRefusal(w, r, err)
			return
		}
		out := DesignSystems(list)
		if out == nil {
			out = DesignSystems{}
		}
		s.writeChecked(w, r, out)
	case "pin":
		var body DesignlyPin
		if !readDesignly(w, r, &body) {
			return
		}
		if err := s.Designly.Pin(ctx, body.ID, body.Pinned); err != nil {
			s.designlyRefusal(w, r, err)
			return
		}
		s.writeChecked(w, r, body)
	default:
		refuse(w, RefuseNotFound)
	}
}

// readDesignly reads the body into v and checks it; false when it answered a bad request.
func readDesignly[T interface{ Validate() error }](w http.ResponseWriter, r *http.Request, v *T) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, DesignlyBodyMax))
	if err == nil {
		err = decodeStrict(raw, v)
	}
	if err == nil {
		err = (*v).Validate()
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, DesignlyRefusal{R: ResultRefused, Code: RefuseBadRequest, Message: err.Error()})
		return false
	}
	return true
}

func (s *Server) designlyRefusal(w http.ResponseWriter, r *http.Request, err error) {
	var de *DesignlyError
	if !errors.As(err, &de) {
		if errors.Is(err, context.DeadlineExceeded) {
			de = &DesignlyError{Code: RefuseTimeout, Message: "timeout"}
		} else {
			jsonlog.Log("warn", "v2: designly failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
			de = &DesignlyError{Code: RefuseInternal, Message: "internal"}
		}
	}
	status := refusalStatus[de.Code]
	if de.Code == RefuseBadRequest {
		status = http.StatusUnprocessableEntity
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}
	out := DesignlyRefusal{R: ResultRefused, Code: de.Code, Message: strings.TrimSpace(de.Message)}
	if out.Message == "" {
		out.Message = string(de.Code)
	}
	if de.ProjectID != "" {
		out.ProjectID = &de.ProjectID
	}
	writeJSON(w, status, out)
}
