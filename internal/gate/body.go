package gate

import "net/http"

// BodyRoute is a route whose answer is not only JSON — a file, a picture, a range of a video —, answered while its
// slice is on.
type BodyRoute struct {
	Slice string
	Match func(*http.Request) bool
	// Serve writes the whole answer, a refusal included.
	Serve http.HandlerFunc
}

// AsBodyRoutes turns body routes into gate routes.
func (f *Gate) AsBodyRoutes(routes ...BodyRoute) []Route {
	out := make([]Route, 0, len(routes))
	for _, br := range routes {
		out = append(out, Route{Slice: br.Slice, Match: br.Match, Handler: br.Serve})
	}
	return out
}
