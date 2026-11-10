package hubproto

import (
	"crypto/ed25519"
	"time"
)

// The hub's store (docs/hub/store.md): the revisions and contents of a space's collections, a Sinopia library first.
// A machine asks for a short session with a request signed by its own key (POST StorePath); the session is the bearer
// of every request to the store's API under StoreAPIPath, so the long polls and uploads of a sync are not each a
// signature the hub must remember.

const (
	// StoreDomain is the first line of the message signed to ask for a store session: a signature made for the dial,
	// the unlink, the sites or the peers is good for nothing here, and this one for nothing else.
	StoreDomain = "harnessly-agent-store-v1"
	// StorePath is where a machine asks for a session.
	StorePath = "/agent/store"
	// StoreAPIPath is the root of the store's API on the hub: /store/v1/collections/<kind>/…
	StoreAPIPath = "/store"
	// StoreCapability is the id the hub's GET /api/capabilities lists the store under; a hub without it has no store,
	// and a runtime then keeps its data as before.
	StoreCapability = "store"
)

// StoreSession is the hub's answer to a machine that asked for a store session.
type StoreSession struct {
	// Token is the bearer of the store's API; it works until ExpiresAt, then the machine asks again.
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
	// Path is the root of the store's API on the hub (StoreAPIPath).
	Path string `json:"path"`
	// Collections are the kinds this machine may open.
	Collections []string `json:"collections"`
	// Person names the machine's person in the apps collection: their user areas are
	// storage/<app>/user/<Person>.json, which no other person's machine sees. "" from a hub that names nobody (an
	// older one, or a machine nobody made): the user areas stay on the machine.
	Person string `json:"person,omitempty"`
}

// SignStore makes the Authorization header value of the request for a store session.
func SignStore(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(StoreDomain, priv, machineID, now, host)
}

// VerifyStore is VerifyDial for the header SignStore made.
func VerifyStore(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(StoreDomain, pub, a, host, now)
}

// SpaceApp is an app installed for a space as the hub lists it from the store (GET /api/spaces/{id}/apps):
// the hub serves its view to the space's members at Path with every machine off.
type SpaceApp struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Targets []string `json:"targets"`
	// Path is the app's page at the hub (/apps/space/<space id>/<app id>/); empty for an app without a view for the app
	// target.
	Path string `json:"path,omitempty"`
}
