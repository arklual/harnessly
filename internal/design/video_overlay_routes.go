package design

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The overlay layer's routes (HRN-653). One route does it all, as the boards' route does: POST {page, action:
// add|set|rm|ls, id?, force?, ...the overlay's fields}. The page's route is a person (their edit is stamped human), the
// CLI's is the agent (its edit is stamped agent, and it may not change what a person wrote unless it says force).

// videoOverlayFieldNames are the body's keys that go into an overlay.
var videoOverlayFieldNames = func() []string {
	out := make([]string, 0, len(videoOverlayKeys))
	for k := range videoOverlayKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}()

// VideoOverlayAnswer is what the overlay routes answer: the scene as it now is, and the overlay the call made or changed.
type VideoOverlayAnswer struct {
	Scene   VideoScene     `json:"scene"`
	Overlay *VideoOverlay  `json:"overlay"`
	List    []VideoOverlay `json:"overlays"`
	// Warnings: the overlay stands on others at the same time (ids in Overlaps). The change is saved all the same.
	Overlaps []string `json:"overlaps,omitempty"`
}

// videoOverlayOp runs one call of the overlay route; the refusal is an error worded for the caller.
func (l *Library) videoOverlayOp(project string, body *jsvalue.Object, actor VideoOverlayActor) (VideoOverlayAnswer, error) {
	page, _ := strField(body, "page")
	action, _ := strField(body, "action")
	oid, _ := strField(body, "id")
	force, _ := Field(body, "force").(bool)
	patch, err := bodyPatch(body, videoOverlayFieldNames...)
	if err != nil {
		return VideoOverlayAnswer{}, deckErr(http400, "The body is not valid", "Тело запроса неверно")
	}
	return l.videoOverlayDo(project, page, action, oid, patch, force, actor)
}

// videoOverlayDo is the overlay route's work, for the route and the agent's tools.
func (l *Library) videoOverlayDo(project, page, action, oid string, patch map[string]json.RawMessage, force bool, actor VideoOverlayActor) (VideoOverlayAnswer, error) {
	if page == "" {
		return VideoOverlayAnswer{}, deckErr(http400, "page is expected", "Нужна page")
	}
	var (
		sc  VideoScene
		err error
	)
	switch action {
	case "ls", "":
		var list []VideoOverlay
		if list, err = l.VideoOverlaysOf(project, page); err != nil {
			return VideoOverlayAnswer{}, err
		}
		if list == nil {
			list = []VideoOverlay{}
		}
		return VideoOverlayAnswer{List: list}, nil
	case "add":
		sc, oid, err = l.AddVideoOverlay(project, page, oid, patch, actor)
	case "set":
		if oid == "" {
			return VideoOverlayAnswer{}, deckErr(http400, "id is expected", "Нужен id надписи")
		}
		sc, err = l.SetVideoOverlay(project, page, oid, patch, actor, force)
	case "rm", "remove":
		if oid == "" {
			return VideoOverlayAnswer{}, deckErr(http400, "id is expected", "Нужен id надписи")
		}
		sc, err = l.RemoveVideoOverlay(project, page, oid, actor, force)
		if err == nil {
			return VideoOverlayAnswer{Scene: sc, List: sc.Overlays}, nil
		}
	default:
		return VideoOverlayAnswer{}, deckErr(http400, "action is add, set, rm or ls", "action — add, set, rm или ls")
	}
	if err != nil {
		return VideoOverlayAnswer{}, err
	}
	out := VideoOverlayAnswer{Scene: sc, List: sc.Overlays}
	if i := overlayIndex(sc.Overlays, oid); i >= 0 {
		out.Overlay = &sc.Overlays[i]
		out.Overlaps = VideoOverlayClashes(sc.Overlays, oid)
	}
	return out, nil
}

// VideoOverlay is POST {artifact, page, action, id?, ...fields} (edit): the VideoOverlayAnswer. The edit is a person's.
func (a *API) VideoOverlay(r *http.Request) Answer {
	body, id, no := a.videoGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	out, err := a.L.videoOverlayOp(id, body, VideoAuthorHuman)
	if err != nil {
		return a.deckFail(r, err, "save the overlay")
	}
	return JSON(r, http.StatusOK, out, noStore())
}

// CLIVideoOverlay is POST {project, page, action, id?, force?, ...fields} (edit; ls: view): {project, ...the answer}.
// The edit is the agent's.
func (a *API) CLIVideoOverlay(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	need := RoleEdit
	if act, _ := strField(body, "action"); act == "ls" || act == "" {
		need = RoleView
	}
	if no := a.gate(r, project, need); no != nil {
		return *no
	}
	if project == "" {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("project is expected", "Нужен project")), h)
	}
	out, err := a.L.videoOverlayOp(project, body, VideoAuthorAgent)
	if err != nil {
		return a.deckFail(r, err, "save the overlay")
	}
	return JSON(r, http.StatusOK, struct {
		Project string `json:"project"`
		VideoOverlayAnswer
	}{project, out}, h)
}
