package ext

import (
	"context"
	"net/http"
	"slices"
)

// Description is the answer of GET /v1/describe, the same for every kind of extension: who the adapter is, which
// protocol it speaks, and what it can do.
type Description struct {
	// Protocol names the kind and the version, `harnessly.<kind>/<major>`, for example harnessly.machine-provider/v1.
	Protocol string `json:"protocol"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	// Capabilities lists what the adapter can do; a call whose capability is not listed is never sent.
	Capabilities []string `json:"capabilities"`
	// ConfigSchema is a JSON Schema of the adapter's own settings; the hub passes it on to its screens and does not
	// read it.
	ConfigSchema map[string]any `json:"config_schema,omitempty"`
}

// Has reports whether the adapter lists a capability.
func (d Description) Has(c string) bool { return slices.Contains(d.Capabilities, c) }

// Describe asks the adapter to describe itself and refuses one that speaks another protocol than want.
func (c *Client) Describe(ctx context.Context, want string) (Description, error) {
	var d Description
	if err := c.Do(ctx, http.MethodGet, "/v1/describe", nil, false, &d); err != nil {
		return Description{}, err
	}
	if d.Protocol != want {
		return Description{}, &Error{Code: "protocol_unsupported", Message: "the adapter speaks " + d.Protocol + ", the hub expects " + want}
	}
	return d, nil
}
