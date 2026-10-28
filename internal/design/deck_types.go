package design

import (
	"context"
	"sync"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// A deck is a project of the kind "slides": one board per slide, in the order of its pages. What a deck adds to a
// project lives in its meta folder, one file per slide or comment (see meta.go: the runtimes sharing a library never
// write one shared file), and its recordings and exports under .media, which the history never takes in.
//
// This file holds the contract the deck's parts are built against: the JSON shapes the routes answer and the files
// they keep. The handlers live in the files of their parts (deck_routes.go, deckpatch.go, deckrec.go, deckjobs.go).

// The deck's files under a project's folder (the project's root, beside project/ and meta/).
const (
	DeckSettingsFile = "meta/deck.json"
	DeckNotesDir     = "meta/notes"
	DeckCommentsDir  = "meta/comments"
	// DeckThemeCSS is written on every theme or font change; the boards link it.
	DeckThemeCSS     = "project/deck-theme.css"
	DeckMediaDir     = ".media"
	DeckRecDir       = ".media/rec"
	DeckExportsDir   = ".media/exports"
	DeckPublishedDir = ".media/published"
)

// DeckTone is how the speaker's notes are written.
type DeckTone string

const (
	DeckToneTalk  DeckTone = "talk"
	DeckToneBiz   DeckTone = "biz"
	DeckToneShort DeckTone = "short"
)

// DeckTones is every DeckTone, in the order the settings show them.
var DeckTones = []DeckTone{DeckToneTalk, DeckToneBiz, DeckToneShort}

// DeckFonts are the font ids of a deck and the CSS stack each stands for.
var DeckFonts = map[string]string{
	"inter": "Inter, system-ui, sans-serif",
	"golos": `"Golos Text", Inter, sans-serif`,
	"serif": `Georgia, "PT Serif", serif`,
}

// The bounds of a deck's settings.
const (
	DeckSlidesMin  = 2
	DeckSlidesMax  = 40
	DeckMinutesMin = 1
	DeckMinutesMax = 180
)

// DeckStyle is where a deck's look came from: a company template (.pptx) read at create, or a design system.
type DeckStyle struct {
	// Source is the template's file name as the person attached it; "" when there was none.
	Source string `json:"source"`
	// System is the design system the deck is built on; nil for none.
	System *string `json:"system"`
	// Layouts is how many slide layouts the template has.
	Layouts int `json:"layouts"`
}

// DeckTheme is a named set of the --deck-* CSS variables.
type DeckTheme struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Vars map[string]string `json:"vars"`
}

// DeckSettings is meta/deck.json: written by the runtime, read by the agent and the page. V is 1.
type DeckSettings struct {
	V int `json:"v"`
	// Minutes 0: the length does not matter, the agent decides; otherwise DeckMinutesMin..DeckMinutesMax.
	Minutes int `json:"minutes"`
	// Slides nil: the agent picks the count ("Сам подберёт"); otherwise DeckSlidesMin..DeckSlidesMax.
	Slides *int       `json:"slides"`
	Tone   DeckTone   `json:"tone"`
	Script bool       `json:"script"`
	Style  *DeckStyle `json:"style"`
	Theme  string     `json:"theme"`
	Font   string     `json:"font"`
	// Themes are the deck's own themes; absent: the built-in ones (DefaultDeckThemes).
	Themes []DeckTheme `json:"themes,omitempty"`
}

// DefaultDeckThemes are the built-in themes: Компания, Фиолет and Лес.
func DefaultDeckThemes() []DeckTheme {
	return []DeckTheme{
		{ID: "co", Name: locale.Pick("Company", "Компания"), Vars: map[string]string{"--deck-bg": "#ffffff", "--deck-ink": "#1c1c1f",
			"--deck-mute": "#4a4a52", "--deck-acc": "#ffdd2d", "--deck-on-acc": "#1c1c1f", "--deck-acc-soft": "#fff6c2",
			"--deck-acc-ink": "#1c1c1f", "--deck-panel": "#f4f4f6", "--deck-line": "#d9d9de"}},
		{ID: "vi", Name: locale.Pick("Violet", "Фиолет"), Vars: map[string]string{"--deck-bg": "#fbfaff", "--deck-ink": "#16161a",
			"--deck-mute": "#3d3d46", "--deck-acc": "#7b61ff", "--deck-on-acc": "#ffffff", "--deck-acc-soft": "#f1eeff",
			"--deck-acc-ink": "#6a4ff0", "--deck-panel": "#f4f2fc", "--deck-line": "#cfc8f5"}},
		{ID: "gr", Name: locale.Pick("Forest", "Лес"), Vars: map[string]string{"--deck-bg": "#f7f6f1", "--deck-ink": "#17221d",
			"--deck-mute": "#3f4a44", "--deck-acc": "#1f6b4f", "--deck-on-acc": "#ffffff", "--deck-acc-soft": "#e2efe8",
			"--deck-acc-ink": "#1f6b4f", "--deck-panel": "#ecebe4", "--deck-line": "#c9d6cf"}},
	}
}

// DefaultDeckSettings are the settings of a deck nobody has set yet.
func DefaultDeckSettings() DeckSettings {
	return DeckSettings{V: 1, Minutes: 10, Tone: DeckToneTalk, Script: true, Theme: "co", Font: "inter"}
}

// DeckNotesSeconds is how long a slide's notes take to say: max(10, round(words / 2.2 / 5) * 5). The page counts the
// same way (deck/notes-len.ts).
func DeckNotesSeconds(words int) int {
	s := int(float64(words)/2.2/5+0.5) * 5
	if s < 10 {
		return 10
	}
	return s
}

// DeckSlide is a slide as the deck answers it: the board's page with its notes.
type DeckSlide struct {
	// ID is the page's id (meta/pages/<id>.json), the key of its notes and comments.
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Notes string `json:"notes"`
	// Voice: the notes came from a dictation (meta/notes/<id>.raw.md is there).
	Voice   bool `json:"voice"`
	Seconds int  `json:"seconds"`
	// Version moves with the board file (its mtime and size): the page keys the slide's picture on it, so a picture
	// drawn while the board was empty is not kept once the agent writes it.
	Version string `json:"version"`
	// Blank: the board has nothing on it yet (the template's empty <main class="board">) — the agent has made the page
	// but not drawn the slide.
	Blank bool `json:"blank"`
}

// DeckPerson is who wrote or resolved something.
type DeckPerson struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// DeckComment is meta/comments/<ULID>.json: a team comment pinned to a point of a slide.
type DeckComment struct {
	ID   string `json:"id"`
	Page string `json:"page"`
	// X and Y are percents of the slide's width and height (0..100).
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// Label is the block under the point ("Заголовок"), "" when none.
	Label      string     `json:"label"`
	Text       string     `json:"text"`
	By         DeckPerson `json:"by"`
	At         string     `json:"at"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *string    `json:"resolvedBy"`
	ResolvedAt *string    `json:"resolvedAt"`
}

// DeckGuestResolution is meta/comments/g-<gatewayId>.json: a guest comment (kept by the gateway) marked resolved here.
type DeckGuestResolution struct {
	Resolved bool   `json:"resolved"`
	By       string `json:"by"`
	At       string `json:"at"`
}

// DeckGuestPrefix starts the id of a guest comment in a resolve: g-<gatewayId>.
const DeckGuestPrefix = "g-"

// DeckRecState is where a recording is.
type DeckRecState string

const (
	DeckRecRecording DeckRecState = "recording"
	DeckRecDone      DeckRecState = "done"
	DeckRecFailed    DeckRecState = "failed"
)

// DeckTake is one take of a slide in a recording; a retake adds one, the last take of a slide wins.
type DeckTake struct {
	Page    string `json:"page"`
	N       int    `json:"n"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
}

// DeckRecMeta is .media/rec/<recId>/rec.json.
type DeckRecMeta struct {
	ID         string       `json:"id"`
	StartedAt  string       `json:"startedAt"`
	DurationMs int64        `json:"durationMs"`
	Camera     bool         `json:"camera"`
	Mime       string       `json:"mime"`
	Slides     []string     `json:"slides"`
	Takes      []DeckTake   `json:"takes"`
	State      DeckRecState `json:"state"`
}

// The kinds of the lines of events.jsonl.
const (
	DeckEvSlide  = "slide"
	DeckEvStep   = "step"
	DeckEvCursor = "cursor"
	DeckEvClick  = "click"
	DeckEvLaser  = "laser"
	DeckEvPause  = "pause"
	DeckEvResume = "resume"
	DeckEvRetake = "retake"
	DeckEvASR    = "asr"
	DeckEvCam    = "cam"
)

// DeckEvent is a line of .media/rec/<recId>/events.jsonl; T is ms from the recording's start. Which fields a line has
// depends on K: slide {page,i}, step {n}, cursor and click {x,y} in board pixels, laser {on}, retake {page},
// asr {t0,text} (a final; t0 is the time of its first partial), cam {x,y,r}.
type DeckEvent struct {
	T    int64    `json:"t"`
	K    string   `json:"k"`
	Page string   `json:"page,omitempty"`
	I    *int     `json:"i,omitempty"`
	N    *int     `json:"n,omitempty"`
	X    *float64 `json:"x,omitempty"`
	Y    *float64 `json:"y,omitempty"`
	R    *float64 `json:"r,omitempty"`
	On   *bool    `json:"on,omitempty"`
	T0   *int64   `json:"t0,omitempty"`
	Text string   `json:"text,omitempty"`
}

// DeckPartKind is what a part of a transcript segment is.
type DeckPartKind string

const (
	DeckPartWord   DeckPartKind = "word"
	DeckPartFiller DeckPartKind = "filler"
	DeckPartPause  DeckPartKind = "pause"
)

// DeckPart is a piece of a segment: words, a filler or a pause. Cut is whether the montage drops it.
type DeckPart struct {
	Text string       `json:"text"`
	Cut  bool         `json:"cut"`
	Kind DeckPartKind `json:"kind"`
	T0   int64        `json:"t0"`
	T1   int64        `json:"t1"`
}

// DeckSegment is one recognised phrase of a recording, on the slide it was said on. Times are ms from the start.
type DeckSegment struct {
	T0    int64      `json:"t0"`
	T1    int64      `json:"t1"`
	Page  string     `json:"page"`
	Parts []DeckPart `json:"parts"`
}

// DeckTranscript is .media/rec/<recId>/transcript.json.
type DeckTranscript struct {
	Segments []DeckSegment `json:"segments"`
}

// DeckEdit is .media/rec/<recId>/edit.json: the montage. Kept are "<segment>.<part>" indexes the person kept although
// the cut would drop them. Cursor is "smooth" or "off"; Cam is the camera circle's corner: "br", "bl", "tr", "tl" or "off".
type DeckEdit struct {
	Cut    bool     `json:"cut"`
	Subs   bool     `json:"subs"`
	Kept   []string `json:"kept"`
	Cursor string   `json:"cursor"`
	Cam    string   `json:"cam"`
}

// DefaultDeckEdit is the montage of a new recording: cut and subtitles on, as the done dialog shows them.
func DefaultDeckEdit() DeckEdit {
	return DeckEdit{Cut: true, Subs: true, Kept: []string{}, Cursor: "smooth", Cam: "br"}
}

// DeckRecording is the answer of GET and PUT /design/deck/rec and of POST /design/deck/rec/finish. Segments are empty
// while the transcript is being built (Summary.State "recording" or the build not done yet). CutMs is how much the
// montage takes out; Retaken are the 1-based numbers of the slides that were taken again.
type DeckRecording struct {
	Summary  DeckRecMeta   `json:"summary"`
	Segments []DeckSegment `json:"segments"`
	Edit     DeckEdit      `json:"edit"`
	CutMs    int64         `json:"cutMs"`
	Retaken  []int         `json:"retaken"`
}

// DeckRecordingInfo is a recording in the deck's state.
type DeckRecordingInfo struct {
	ID         string       `json:"id"`
	At         string       `json:"at"`
	DurationMs int64        `json:"durationMs"`
	State      DeckRecState `json:"state"`
	Retaken    []int        `json:"retaken"`
}

// DeckChapter is a slide's start in the published video (.media/published/chapters.json); T is ms.
type DeckChapter struct {
	Page  string `json:"page"`
	Title string `json:"title"`
	T     int64  `json:"t"`
}

// DeckPublished says what guests may read: .media/published/video.mp4 and deck.pdf. PDFAuto: the PDF is kept up to
// date for guests (deckpdfauto.go); PDFBuilding: a newer one is being built or is about to be; PDFAt: when the
// published one was built from (RFC 3339), null without one; PDFError: why the last build failed, null when it did not.
type DeckPublished struct {
	Video       bool    `json:"video"`
	PDF         bool    `json:"pdf"`
	PDFAuto     bool    `json:"pdfAuto"`
	PDFBuilding bool    `json:"pdfBuilding"`
	PDFAt       *string `json:"pdfAt"`
	PDFError    *string `json:"pdfError"`
}

// DeckState is the answer of GET /design/deck. Rev is the newest mtime (ms) of the deck's files: the page polls every
// 3 s while visible and right after its own writes, and redraws when it moved. Chrome and Ffmpeg say whether the
// exports that need them can run on this machine.
type DeckState struct {
	Rev           int64               `json:"rev"`
	Settings      DeckSettings        `json:"settings"`
	Chrome        bool                `json:"chrome"`
	Ffmpeg        bool                `json:"ffmpeg"`
	Slides        []DeckSlide         `json:"slides"`
	Comments      []DeckComment       `json:"comments"`
	GuestResolved []string            `json:"guestResolved"`
	Recordings    []DeckRecordingInfo `json:"recordings"`
	Published     DeckPublished       `json:"published"`
}

// DeckPatchOp is what a hand edit of a board does.
type DeckPatchOp string

const (
	DeckPatchText  DeckPatchOp = "text"
	DeckPatchStyle DeckPatchOp = "style"
	DeckPatchMove  DeckPatchOp = "move"
	DeckPatchReset DeckPatchOp = "reset"
)

// DeckPatchRequest is the body of POST /design/deck/patch. Tpl is the element's data-dc-tpl (preorder index of the
// template's start tags). text: Was is the element's inner HTML as the page saw it, HTML the new one (b strong i em u
// s br and text only). style: Style sets (a value) or removes (null) CSS properties of the style attribute. move:
// X and Y in board pixels become `translate` (in cqw of the board's width; in px when Unit is "px", which the board
// asks for a block inside its own size container). reset: removes translate. For style, move and reset Was is the
// element's fingerprint as the board gave it (`was` of tk:deck-hover, -select, -drag): its source tag, style attribute
// and words; the edit lands on that element or is refused as changed.
type DeckPatchRequest struct {
	Artifact string             `json:"artifact"`
	Page     string             `json:"page"`
	Tpl      int                `json:"tpl"`
	Op       DeckPatchOp        `json:"op"`
	Was      *string            `json:"was,omitempty"`
	HTML     *string            `json:"html,omitempty"`
	Style    map[string]*string `json:"style,omitempty"`
	X        *float64           `json:"x,omitempty"`
	Y        *float64           `json:"y,omitempty"`
	Unit     string             `json:"unit,omitempty"`
}

// The codes a refused patch answers with: 409 changed (the source is not what the page saw), 422 dynamic (the
// element's content or style is interpolated).
const (
	DeckPatchChanged = "changed"
	DeckPatchDynamic = "dynamic"
)

// DeckPptxStyle is the answer of POST /design/deck/pptx-style: what a company template gives a new deck.
type DeckPptxStyle struct {
	Name    string            `json:"name"`
	Colors  []string          `json:"colors"`
	Fonts   DeckPptxFonts     `json:"fonts"`
	Layouts int               `json:"layouts"`
	Vars    map[string]string `json:"vars"`
}

// DeckPptxFonts are a template's heading and body fonts.
type DeckPptxFonts struct {
	Head string `json:"head"`
	Body string `json:"body"`
}

// DeckTidied is a slide's notes made from a dictation, in the answer of POST /design/deck/tidy.
type DeckTidied struct {
	ID      string `json:"id"`
	Notes   string `json:"notes"`
	Seconds int    `json:"seconds"`
	Voice   bool   `json:"voice"`
}

// DeckExportFormat is a file a deck can be exported to.
type DeckExportFormat string

const (
	DeckExportPDF  DeckExportFormat = "pdf"
	DeckExportPNG  DeckExportFormat = "png"
	DeckExportPPTX DeckExportFormat = "pptx"
	DeckExportHTML DeckExportFormat = "html"
	DeckExportMP4  DeckExportFormat = "mp4"
)

// DeckExportFormats is every format, in the order of the «Ещё» menu's items.
var DeckExportFormats = []DeckExportFormat{DeckExportPDF, DeckExportPPTX, DeckExportMP4, DeckExportPNG, DeckExportHTML}

// DeckJobState is where an export job is.
type DeckJobState string

const (
	DeckJobQueued  DeckJobState = "queued"
	DeckJobRunning DeckJobState = "running"
	DeckJobDone    DeckJobState = "done"
	DeckJobFailed  DeckJobState = "failed"
)

// DeckExportJob is an export job (kept in memory, keyed <artifact>/<id>), the answer of POST and GET
// /design/deck/export. File is the file's name once done; Error is the reason, worded for the person, once failed.
type DeckExportJob struct {
	ID       string           `json:"id"`
	Format   DeckExportFormat `json:"format"`
	State    DeckJobState     `json:"state"`
	Progress float64          `json:"progress"`
	File     *string          `json:"file"`
	Error    *string          `json:"error"`
	At       string           `json:"at"`
}

// DeckExportRun is what an exporter is given: the project, the recording (mp4 only), the file to write and where to
// report progress (0..1).
type DeckExportRun struct {
	L        *Library
	Artifact string
	Rec      string
	Out      string
	Progress func(float64)
}

// DeckExporter writes one export. Needs names what it runs ("chrome", "ffmpeg"): a machine without it fails the job
// at once with a worded reason.
type DeckExporter struct {
	Needs []string
	Ext   string
	Run   func(ctx context.Context, run DeckExportRun) error
}

var (
	deckExportersMu sync.Mutex
	deckExporters   = map[DeckExportFormat]DeckExporter{}
)

// RegisterDeckExporter adds the exporter of a format; the parts register theirs from init.
func RegisterDeckExporter(f DeckExportFormat, e DeckExporter) {
	deckExportersMu.Lock()
	defer deckExportersMu.Unlock()
	deckExporters[f] = e
}

// DeckExporterOf is the exporter of a format; false when none is registered.
func DeckExporterOf(f DeckExportFormat) (DeckExporter, bool) {
	deckExportersMu.Lock()
	defer deckExportersMu.Unlock()
	e, ok := deckExporters[f]
	return e, ok
}

// DeckExportersRegistered are the formats that have an exporter, in DeckExportFormats' order.
func DeckExportersRegistered() []DeckExportFormat {
	deckExportersMu.Lock()
	defer deckExportersMu.Unlock()
	out := []DeckExportFormat{}
	for _, f := range DeckExportFormats {
		if _, ok := deckExporters[f]; ok {
			out = append(out, f)
		}
	}
	return out
}
