package design

import (
	"context"
	"time"
)

// A video is a deck folder with one more file: meta/video.json. It keeps everything a deck has (theme,
// deck-theme.css, notes, comments, share) and adds scenes with durations and timed moments, rendered to MP4 in one or
// two formats. Every board is one scene, in the order of the project's pages.
//
// This file holds the contract the video's parts are built against: the JSON shapes the routes answer, the files they
// keep and the engine's frame interface. The handlers live in the files of their parts (video_routes.go, video_cli.go,
// videoshoot.go, videocheck.go, videorender.go, videorender_routes.go). Times are integer milliseconds everywhere.

// The video's files under a project's folder, beside the deck's (deck_types.go).
const (
	VideoSettingsFile = "meta/video.json"
	// VideoScenesDir holds <pageId>.json (VideoSceneMeta), one per scene that has anything set.
	VideoScenesDir = "meta/scenes"
	// VideoCacheDir is the render cache: scenes/<key>.mp4, frames/<key>.jpg, renders/<slug>/<hash>.mp4.
	VideoCacheDir        = ".media/video"
	VideoSceneClipsDir   = ".media/video/scenes"
	VideoFramesDir       = ".media/video/frames"
	VideoRendersDir      = ".media/video/renders"
	VideoChaptersFile    = DeckPublishedDir + "/chapters.json"
	VideoPublishedFile   = DeckPublishedDir + "/video.json"
	VideoRuntimeVersion  = 2
	VideoRendersKept     = 3
	VideoCacheMaxBytes   = 2 << 30
	VideoRenderTabs      = 2
	VideoRenderTimeout   = 40 * time.Minute
	VideoDraftCRF        = 28
	VideoFinalCRF        = 16
	VideoFrameJPEGQ      = 92
	VideoLoadPerCPUPause = 1.5
)

// The bounds of a video's settings and scenes (D2, D5).
const (
	VideoFPS             = 30
	VideoSecondsMin      = 15
	VideoSecondsMax      = 300
	VideoSceneDurMin     = 1000
	VideoSceneDurMax     = 60000
	VideoTransitionMsMax = 2000
	// VideoAutoMin is the shortest automatic scene; VideoMomentTail and VideoEndHold follow the last moment's end.
	VideoAutoMin    = 3000
	VideoMomentTail = 600
	VideoEndHold    = 1500
	// VideoWordsPerSecond is how fast a viewer reads a scene's text (meta/notes).
	VideoWordsPerSecond = 2.5
)

// VideoAutoDur is a scene's automatic duration (D5): clamp(round100(max(3000, lastMomentEnd + 600 + 1500,
// words / 2.5 × 1000)), 1000, 60000). Durations are computed only in Go; the page lays out what it is given.
func VideoAutoDur(lastMomentEnd int64, words int) int64 {
	d := float64(VideoAutoMin)
	if m := float64(lastMomentEnd + VideoMomentTail + VideoEndHold); m > d {
		d = m
	}
	if w := float64(words) / VideoWordsPerSecond * 1000; w > d {
		d = w
	}
	ms := int64(d/100+0.5) * 100
	return min(max(ms, VideoSceneDurMin), VideoSceneDurMax)
}

// VideoFormat is a video's aspect: 16:9 (1920×1080) or 9:16 (1080×1920).
type VideoFormat string

const (
	VideoLandscape VideoFormat = "16:9"
	VideoPortrait  VideoFormat = "9:16"
)

// VideoFormats is every VideoFormat, in the order the page shows them.
var VideoFormats = []VideoFormat{VideoLandscape, VideoPortrait}

// Valid: f is one of VideoFormats.
func (f VideoFormat) Valid() bool { return f == VideoLandscape || f == VideoPortrait }

// Size is the frame size in pixels; a draft is half of it (960×540, 540×960).
func (f VideoFormat) Size(draft bool) (w, h int) {
	w, h = 1920, 1080
	if f == VideoPortrait {
		w, h = 1080, 1920
	}
	if draft {
		return w / 2, h / 2
	}
	return w, h
}

// Slug is the format in a file or folder name: 16x9, 9x16.
func (f VideoFormat) Slug() string {
	if f == VideoPortrait {
		return "9x16"
	}
	return "16x9"
}

// VideoPublishedName is the published MP4 of a format under DeckPublishedDir: video.mp4 for 16:9 (the name guests of a
// deck already read), video-9x16.mp4 for 9:16.
func VideoPublishedName(f VideoFormat) string {
	if f == VideoPortrait {
		return "video-9x16.mp4"
	}
	return "video.mp4"
}

// VideoVoice is the video's sound. The MVP has none (D10).
type VideoVoice string

const VideoVoiceNone VideoVoice = "none"

// VideoSettings is meta/video.json: written by the runtime, read by the agent and the page. V is 1. Formats has one or
// both of VideoFormats, in that order. Seconds nil: the length does not matter; otherwise VideoSecondsMin..Max. Subs:
// the scene's text is burned in as subtitles and shown over the live player.
type VideoSettings struct {
	V       int           `json:"v"`
	Formats []VideoFormat `json:"formats"`
	FPS     int           `json:"fps"`
	Seconds *int          `json:"seconds"`
	Voice   VideoVoice    `json:"voice"`
	Subs    bool          `json:"subs"`
}

// DefaultVideoSettings are the settings of a video nobody has set: 16:9, length free, subtitles on.
func DefaultVideoSettings() VideoSettings {
	return VideoSettings{V: 1, Formats: []VideoFormat{VideoLandscape}, FPS: VideoFPS, Voice: VideoVoiceNone, Subs: true}
}

// VideoTransitionKind is how a scene comes in. The board may also say data-transition="push|magic" on <main>; the MVP
// plays both as fade (D3).
type VideoTransitionKind string

const (
	VideoTransitionNone VideoTransitionKind = "none"
	VideoTransitionFade VideoTransitionKind = "fade"
)

// VideoTransitionKinds is every VideoTransitionKind.
var VideoTransitionKinds = []VideoTransitionKind{VideoTransitionNone, VideoTransitionFade}

// VideoTransition is a scene's way in: a fade from --deck-bg over Ms (0..VideoTransitionMsMax) inside the scene itself;
// scenes never overlap.
type VideoTransition struct {
	Kind VideoTransitionKind `json:"kind"`
	Ms   int                 `json:"ms"`
}

// VideoSceneMeta is meta/scenes/<pageId>.json. Dur nil: automatic (VideoAutoDur); Transition nil: the board's own
// data-transition, else none.
type VideoSceneMeta struct {
	V          int              `json:"v"`
	Dur        *int64           `json:"dur"`
	Transition *VideoTransition `json:"transition"`
}

// The build-ins a block may take (data-build-in), applied by video-runtime.js as a pure function of time (D4).
const (
	VideoBuildInFade = "fade"
	VideoBuildInRise = "rise"
	VideoBuildInPop  = "pop"
)

// VideoBuildIns is every build-in.
var VideoBuildIns = []string{VideoBuildInFade, VideoBuildInRise, VideoBuildInPop}

// VideoMoment is a timed block of a scene, read from the board's source: data-block, data-tk-at / data-tk-out (seconds
// from the scene's start, kept here as ms) and data-build-in. Tpl is the element's data-dc-tpl when known. Out nil: the
// block stays to the scene's end. Derived: the time was not written on the board but derived (from data-step).
type VideoMoment struct {
	Block   string `json:"block"`
	Tpl     *int   `json:"tpl"`
	At      int64  `json:"at"`
	Out     *int64 `json:"out"`
	BuildIn string `json:"buildIn"`
	Derived bool   `json:"derived"`
}

// VideoScene is a scene as the video answers it: a board's page with its place on the time line. Start is the sum of the
// earlier scenes' Dur. Auto: Dur came from VideoAutoDur, not meta/scenes. Words and Notes are the scene's text
// (meta/notes, which are also the subtitles). Version and Blank as DeckSlide's. Keys is the scene clip's cache key per
// enabled format (the render's inputs; see videorender.go).
type VideoScene struct {
	ID         string                 `json:"id"`
	Path       string                 `json:"path"`
	Title      string                 `json:"title"`
	Start      int64                  `json:"start"`
	Dur        int64                  `json:"dur"`
	Auto       bool                   `json:"auto"`
	Transition VideoTransition        `json:"transition"`
	Moments    []VideoMoment          `json:"moments"`
	Words      int                    `json:"words"`
	Notes      string                 `json:"notes"`
	Version    string                 `json:"version"`
	Blank      bool                   `json:"blank"`
	Keys       map[VideoFormat]string `json:"keys"`
}

// VideoPublishedFormat is one format's entry of .media/published/video.json: the inputs key it was rendered from, its
// length and when it was published (RFC 3339).
type VideoPublishedFormat struct {
	Key   string `json:"key"`
	DurMs int64  `json:"durMs"`
	At    string `json:"at"`
}

// VideoPublished is .media/published/video.json (D7).
type VideoPublished struct {
	Formats map[VideoFormat]VideoPublishedFormat `json:"formats"`
}

// VideoRenderInfo is what the page shows for a format's published MP4. Published: the file is there. Key, DurMs, At are
// the published entry's, null when there is none. CurrentKey is the key of today's inputs; Fresh: Key == CurrentKey.
type VideoRenderInfo struct {
	Format     VideoFormat `json:"format"`
	Published  bool        `json:"published"`
	Key        *string     `json:"key"`
	CurrentKey string      `json:"currentKey"`
	Fresh      bool        `json:"fresh"`
	DurMs      *int64      `json:"durMs"`
	At         *string     `json:"at"`
}

// VideoRenderRequest is the body of POST /design/video/render (and the CLI's, with project) less the artifact. Page nil:
// the whole video; a page renders only that scene's clip (for a look, never published). Draft: half size, crf 28,
// never published.
type VideoRenderRequest struct {
	Format VideoFormat `json:"format"`
	Page   *string     `json:"page"`
	Draft  bool        `json:"draft"`
}

// VideoRenderJob is a render job (kept in memory, keyed <artifact>/<id>). Progress is 0..1. File is the file's name
// once done; Error is the reason, worded for the person, once failed. At is when it was asked (RFC 3339).
type VideoRenderJob struct {
	ID       string       `json:"id"`
	Format   VideoFormat  `json:"format"`
	Scene    *string      `json:"scene"`
	Draft    bool         `json:"draft"`
	State    DeckJobState `json:"state"`
	Progress float64      `json:"progress"`
	File     *string      `json:"file"`
	Error    *string      `json:"error"`
	At       string       `json:"at"`
}

// VideoCheckLevel is how bad a check's finding is.
type VideoCheckLevel string

const (
	VideoCheckError VideoCheckLevel = "error"
	VideoCheckWarn  VideoCheckLevel = "warn"
)

// The codes of a check's findings.
const (
	// Static lint of the board's scripts.
	VideoCheckTimer   = "timer"          // setTimeout, setInterval
	VideoCheckRaf     = "raf"            // the board's own requestAnimationFrame loop
	VideoCheckUnbound = "motion-unbound" // GSAP on the board outside tkVideo.motion
	VideoCheckRandom  = "random"         // Math.random
	VideoCheckClock   = "clock"          // Date.now, new Date(
	// Times.
	VideoCheckPastEnd = "past-end" // a data-tk-at past the scene's duration
	// In Chrome, at the scene's end, per enabled format.
	VideoCheckOverflow  = "overflow"        // text overflows its box
	VideoCheckOffscreen = "offscreen"       // a block outside the viewport
	VideoCheckSmallText = "small-text"      // a font under 28 px at 1080 width (9:16)
	VideoCheckUnstable  = "unstable"        // the same t captured twice gives different frames
	VideoCheckOverlap   = "motion-overlap"  // two tweens on one property of one target at one time
	VideoCheckMotionEnd = "motion-past-end" // the motion runs past the scene's end
	VideoCheckExternal  = "external"        // the board reaches outside the project and the kit
	VideoCheckAsset     = "asset"           // a file of the board did not load
	VideoCheckScript    = "script"          // a script of the board threw
	VideoCheckEngine    = "engine"          // an engine of the board failed
)

// VideoCheckIssue is one finding of POST /api/cli/design/video/check. Format nil: the finding holds for every format
// (the static lint); At nil: not tied to a moment; Block nil: not tied to a data-block.
type VideoCheckIssue struct {
	Scene  string          `json:"scene"`
	Format *VideoFormat    `json:"format"`
	At     *int64          `json:"at"`
	Block  *string         `json:"block"`
	Code   string          `json:"code"`
	Level  VideoCheckLevel `json:"level"`
	Msg    string          `json:"msg"`
}

// VideoState is the answer of GET /design/video. Rev is the newest mtime (ms) of the deck's files, meta/video.json,
// meta/scenes and the published video.json: the page polls every 3 s while visible and redraws when it moved. DurMs is
// the sum of the scenes' Dur. Renders has one entry per enabled format. Job is the running render, null when none.
type VideoState struct {
	Rev           int64             `json:"rev"`
	Settings      VideoSettings     `json:"settings"`
	Deck          DeckSettings      `json:"deck"`
	Chrome        bool              `json:"chrome"`
	Ffmpeg        bool              `json:"ffmpeg"`
	DurMs         int64             `json:"durMs"`
	Scenes        []VideoScene      `json:"scenes"`
	Comments      []DeckComment     `json:"comments"`
	GuestResolved []string          `json:"guestResolved"`
	Renders       []VideoRenderInfo `json:"renders"`
	Job           *VideoRenderJob   `json:"job"`
}

// DeckCommentVideo is DeckComment.View for a comment on a moment or a stretch of a video.
const DeckCommentVideo = "video"

// videoShot is one frame of videoFrames: I is its index in times, T its time (ms from the scene's start), JPEG its
// bytes. Same: nothing changed since the previous frame (JPEG is then the previous frame's bytes, so the render can pipe
// it again without encoding a new one).
type videoShot struct {
	I     int
	T     int64
	Image []byte // a JPEG, or a PNG when the tab is lossless
	Same  bool
}

// videoFramesImpl is the engine the render and the frame route call (WP2a builds Library.videoFrames in videoshoot.go):
// it opens the scene's board in tab at f's size and seeks it to each of times (ms), calling each in order. WP2b's
// tests swap in a fake.
var videoFramesImpl = (*Library).videoFrames

// compile-time shape of the engine interface.
var _ func(*Library, context.Context, *deckTab, string, VideoScene, VideoFormat, bool, []int64, func(videoShot) error) error = videoFramesImpl
