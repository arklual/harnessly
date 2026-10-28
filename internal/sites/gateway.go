package sites

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// GatewayAuthority is the authority of a standalone sites server: management requests carry an admin token, and
// visitors of team-only sites come with tickets that the gateway in front of it signs (HMACTickets). The gateway
// signs the person in by its own means and decides who may open which site; the server only trusts its tickets.
type GatewayAuthority struct {
	// AdminToken is the bearer secret that may manage every space's sites. Empty: no management over HTTP.
	AdminToken string
	// LoginURL is the gateway's entry for a visitor who is not signed in; it gets ?host=…&return=….
	LoginURL string
	*HMACTickets
}

func (g *GatewayAuthority) Person(r *http.Request) (*Person, error) {
	b := bearer(r)
	if g.AdminToken == "" || b == "" || subtle.ConstantTimeCompare([]byte(b), []byte(g.AdminToken)) != 1 {
		return nil, nil
	}
	return &Person{ID: "admin", Name: "admin"}, nil
}

// Role: the admin token manages every space.
func (g *GatewayAuthority) Role(context.Context, string, string) (string, error) {
	return RoleOwner, nil
}

// MachineOK: a standalone server has no machines.
func (g *GatewayAuthority) MachineOK(context.Context, string, string, string) bool { return false }

// CanView: the gateway decided before it made the ticket.
func (g *GatewayAuthority) CanView(context.Context, string, string) (bool, error) { return true, nil }

func (g *GatewayAuthority) ViewerEntry(host, returnURL string) string {
	if g.LoginURL == "" {
		return "/"
	}
	sep := "?"
	if strings.Contains(g.LoginURL, "?") {
		sep = "&"
	}
	return g.LoginURL + sep + "host=" + url.QueryEscape(host) + "&return=" + url.QueryEscape(returnURL)
}

func (g *GatewayAuthority) IssueTicket(personID, host string) string {
	return g.HMACTickets.Issue(personID, host)
}
