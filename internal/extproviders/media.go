package extproviders

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Media providers: a provider that makes video and pictures by a request that runs on for a while, not text. Its models
// are a catalog this package keeps (the provider has no list to ask for), and a check of its key asks it for the price
// of one request, which costs nothing. What runs on it is internal/mediagen.

// HiggsfieldBase is the address of the Higgsfield API (docs.higgsfield.ai, «Base URL»; the keys are made in
// console.higgsfield.ai).
const HiggsfieldBase = "https://api.higgsfield.ai"

// MediaKind is what a model makes.
type MediaKind string

const (
	KindVideo MediaKind = "video"
	KindImage MediaKind = "image"
)

// MediaModel is one model of a media provider: its endpoint (the model's ID), what it makes, from what, and which
// settings of the request it takes.
type MediaModel struct {
	// ID is the endpoint ID of the model at the provider (kling-video/v3.0/std/image-to-video).
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Kind  MediaKind `json:"kind"`
	// Family is the vendor's name of the model (kling, seedance, ltx, soul), the short word agents pick by.
	Family string `json:"family"`
	// FromImage: the model takes the first frame (image_url); one that does not is text to video or to picture.
	FromImage bool `json:"fromImage,omitempty"`
	// LastFrame: the model takes the last frame too (end_image_url).
	LastFrame bool `json:"lastFrame,omitempty"`
	// MinSec and MaxSec: any whole number of seconds between them is a length of a clip the model takes; OnlySec: the lengths
	// it takes when it takes some only. All empty for a picture.
	MinSec  int   `json:"minSec,omitempty"`
	MaxSec  int   `json:"maxSec,omitempty"`
	OnlySec []int `json:"onlySec,omitempty"`
	// Audio names the field that asks for the sound of a clip ("sound": on/off, "generate_audio": true/false); "" for a
	// model without.
	Audio string `json:"audio,omitempty"`
	// Ratios are the aspect ratios it takes (empty: the frame follows the image).
	Ratios []string `json:"ratios,omitempty"`
	// Resolutions are the sizes it takes; the first is the cheapest.
	Resolutions []string `json:"resolutions,omitempty"`
}

// HiggsfieldCatalog is the models of Higgsfield an agent may pick, as docs.higgsfield.ai documents them. The provider
// lists 80; the ones that are not here are named by their endpoint ID and get the common fields of a request only.
var HiggsfieldCatalog = []MediaModel{
	{ID: "kling-video/v3.0/std/image-to-video", Label: "Kling 3.0 Std · image → video", Kind: KindVideo, Family: "kling", FromImage: true, MinSec: 3, MaxSec: 15, Audio: "sound"},
	{ID: "kling-video/v3.0/pro/image-to-video", Label: "Kling 3.0 Pro · image → video", Kind: KindVideo, Family: "kling", FromImage: true, MinSec: 3, MaxSec: 15, Audio: "sound"},
	{ID: "kling-video/v3.0-turbo/image-to-video", Label: "Kling 3.0 Turbo · image → video", Kind: KindVideo, Family: "kling", FromImage: true, MinSec: 3, MaxSec: 15, Resolutions: []string{"720p", "1080p"}},
	{ID: "kling-video/v2.5-turbo/standard/image-to-video", Label: "Kling 2.5 Turbo Std · image → video", Kind: KindVideo, Family: "kling", FromImage: true, OnlySec: []int{5, 10}},
	{ID: "kling-video/v3.0/std/text-to-video", Label: "Kling 3.0 Std · text → video", Kind: KindVideo, Family: "kling", MinSec: 3, MaxSec: 15, Audio: "sound", Ratios: []string{"16:9", "9:16", "1:1"}},
	{ID: "bytedance/seedance-2.5/image-to-video", Label: "Seedance 2.5 · image → video", Kind: KindVideo, Family: "seedance", FromImage: true, LastFrame: true, MinSec: 4, MaxSec: 30, Audio: "generate_audio", Resolutions: []string{"480p", "720p", "1080p"}},
	{ID: "bytedance/seedance-2.5/text-to-video", Label: "Seedance 2.5 · text → video", Kind: KindVideo, Family: "seedance", MinSec: 4, MaxSec: 30, Audio: "generate_audio", Resolutions: []string{"480p", "720p", "1080p"}},
	{ID: "lightricks/ltx-2.5/image-to-video/fast", Label: "LTX 2.5 Fast · image → video", Kind: KindVideo, Family: "ltx", FromImage: true, LastFrame: true, OnlySec: []int{6, 8, 10}, Audio: "generate_audio", Ratios: []string{"16:9", "9:16"}, Resolutions: []string{"720p", "1080p", "2k", "4k"}},
	{ID: "lightricks/ltx-2.5/text-to-video/fast", Label: "LTX 2.5 Fast · text → video", Kind: KindVideo, Family: "ltx", OnlySec: []int{6, 8, 10}, Audio: "generate_audio", Ratios: []string{"16:9", "9:16"}, Resolutions: []string{"720p", "1080p", "2k", "4k"}},
	{ID: "higgsfield-ai/soul/v2/standard", Label: "Soul 2.0 · picture", Kind: KindImage, Family: "soul", Ratios: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "2:3", "3:2"}, Resolutions: []string{"720p", "1080p"}},
	{ID: "z-image/turbo", Label: "Z-Image Turbo · picture", Kind: KindImage, Family: "z-image", Ratios: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "2:3", "3:2"}, Resolutions: []string{"1k", "2k"}},
}

// The models a request starts on when it names none.
const (
	DefaultVideoModel = "kling-video/v3.0/std/image-to-video"
	DefaultTextVideo  = "kling-video/v3.0/std/text-to-video"
	DefaultImageModel = "higgsfield-ai/soul/v2/standard"
	// checkModel is the model the key is checked on: the price of its request is asked, nothing is made.
	checkModel = "lightricks/ltx-2.5/text-to-video/fast"
)

// FindMediaModel is the catalog's model of that endpoint ID, or the first of the vendor's family (kling, seedance, ltx,
// soul) that makes kind from an image or not.
func FindMediaModel(ref string, kind MediaKind, fromImage bool) (MediaModel, bool) {
	ref = strings.TrimSpace(ref)
	for _, m := range HiggsfieldCatalog {
		if m.ID == ref {
			return m, true
		}
	}
	for _, m := range HiggsfieldCatalog {
		if strings.EqualFold(m.Family, ref) && m.Kind == kind && (kind == KindImage || m.FromImage == fromImage) {
			return m, true
		}
	}
	return MediaModel{}, false
}

// HiggsfieldModels is the catalog as the models of a provider of the registry.
func HiggsfieldModels() []Model {
	out := make([]Model, 0, len(HiggsfieldCatalog))
	for _, m := range HiggsfieldCatalog {
		out = append(out, Model{ID: m.ID, Label: m.Label})
	}
	return out
}

// ValidMediaKey says whether s is a key ID and a secret, "id:secret": both not empty, no spaces.
func ValidMediaKey(s string) bool {
	id, secret, ok := strings.Cut(s, ":")
	return ok && id != "" && secret != "" && !strings.ContainsAny(s, " \t")
}

// cleanKeyFor is cleanKey for a provider of that API: a Higgsfield key is the key ID and the secret.
func cleanKeyFor(api API, s string) (string, error) {
	key, err := cleanKey(s)
	if err != nil {
		return "", err
	}
	if api == APIHiggsfield && key != "" && !ValidMediaKey(key) {
		return "", refuse(ReasonBadMediaKey)
	}
	return key, nil
}

// checkHiggsfield asks Higgsfield the price of one request: that needs a good key and costs nothing. The API has no
// call for the balance, so the check says the price came; a 403 that is not a refused key is an empty balance.
func (p *Prober) checkHiggsfield(ctx context.Context, base, key string, start time.Time) Check {
	ms := func() int64 { return time.Since(start).Milliseconds() }
	body, _ := json.Marshal(map[string]any{"prompt": "check", "duration": 6})
	status, raw, hdr, err := p.doHeader(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/estimate/"+checkModel, Headers(APIHiggsfield, key), body)
	if err != nil {
		return failure(CheckUnreachable, 0, ms())
	}
	switch {
	case status == http.StatusUnauthorized:
		return failure(CheckUnauthorized, status, ms())
	case status == http.StatusForbidden && strings.Contains(hdr.Get("Cache-Control"), "no-store"):
		// A disabled or expired key (docs.higgsfield.ai, «Headers»).
		return failure(CheckUnauthorized, status, ms())
	case status == http.StatusForbidden:
		c := passed(HiggsfieldModels(), status, ms())
		c.Message = locale.Pick("Connected, but the account's balance is empty: top it up in the Higgsfield console.", "Подключено, но баланс аккаунта пуст: пополните его в консоли Higgsfield.")
		return c
	case status == http.StatusTooManyRequests:
		return failure(CheckRateLimited, status, ms())
	case status >= 500:
		return failure(CheckRejected, status, ms())
	case status >= 200 && status < 300:
		c := passed(HiggsfieldModels(), status, ms())
		c.Message = locale.Pick("Connected. The key works; the balance is in the Higgsfield console (the API does not give it).", "Подключено. Ключ работает; баланс виден в консоли Higgsfield (API его не отдаёт).")
		return c
	case status == http.StatusNotFound || status == http.StatusUnprocessableEntity || status == http.StatusBadRequest:
		// The key was accepted (it is no 401); the request of the check was not, e.g. a model that moved.
		if looksLikeAPI(raw) {
			return passed(HiggsfieldModels(), status, ms())
		}
		return failure(CheckWrongAPI, status, ms())
	}
	return failure(CheckRejected, status, ms())
}

// looksLikeAPI: the body is a FastAPI error ({"detail": …}), the form Higgsfield answers with.
func looksLikeAPI(raw []byte) bool {
	var doc struct {
		Detail json.RawMessage `json:"detail"`
	}
	return json.Unmarshal(raw, &doc) == nil && len(doc.Detail) > 0
}

// HasMediaModel says whether the catalog or the provider's own list (a model the person added) has that ID.
func (p Provider) HasMediaModel(id string) bool {
	return slices.ContainsFunc(HiggsfieldCatalog, func(m MediaModel) bool { return m.ID == id }) || p.HasModel(id)
}
