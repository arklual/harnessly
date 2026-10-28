package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// A deck's routes (slice "design"): a project of the kind "slides" with its notes, comments, hand edits, recordings
// and exports. The JSON routes answer by method inside their handler; the body routes carry bytes (a template, a
// recording's chunks and media, an export's file).

func init() {
	routeGroups = append(routeGroups, designDeckRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designDeckBodyRoutes)
}

// deckPath is a deck route's path under the platform's API.
func deckPath(p string) string { return platformAPI + "/design/deck" + p }

func designDeckRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	if a.OneShot == nil {
		a.OneShot = deckOneShot(d)
	}
	return []gate.JSONRoute{
		designRoute(deckPath(""), a.Deck),
		designRoute(deckPath("/settings"), a.DeckSettings),
		designRoute(deckPath("/notes"), a.DeckNotes),
		designRoute(deckPath("/tidy"), a.DeckTidy),
		designRoute(deckPath("/patch"), a.DeckPatch),
		designRoute(deckPath("/comments"), a.DeckComments),
		designRoute(deckPath("/rec"), a.DeckRec),
		designRoute(deckPath("/rec/events"), a.DeckRecEvents),
		designRoute(deckPath("/rec/finish"), a.DeckRecFinish),
		designRoute(deckPath("/rec/edit"), a.DeckRecEdit),
		designRoute(deckPath("/export"), a.DeckExport),
		designRoute(deckPath("/pdf"), a.DeckPdf),
	}
}

func designDeckBodyRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	at := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return files.RouteIs(r, deckPath(p)) }
	}
	return []gate.BodyRoute{
		{Slice: "design", Match: at("/pptx-style"), Serve: a.DeckPptxStyle},
		{Slice: "design", Match: at("/rec/chunk"), Serve: a.DeckRecChunk},
		{Slice: "design", Match: at("/rec/media"), Serve: a.DeckRecMedia},
		{Slice: "design", Match: at("/export/file"), Serve: a.DeckExportFile},
	}
}
