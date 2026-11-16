package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HubSend hands one event to the hub's rules as POST /automations/hook takes it: {source, fingerprint, event}. The
// runtime sends it through the machine's agent on its hub link (agent.SendAutomationEvent); nil means a hub took it.
type HubSend func(ctx context.Context, source, fingerprint string, event json.RawMessage) error

// Hub is the destination of the hub's rules: every envelope, without its title (HubEvent).
func Hub(send HubSend) Destination { return hubDest{send} }

type hubDest struct{ send HubSend }

func (hubDest) Name() string        { return "hub" }
func (hubDest) Wants(Envelope) bool { return true }

func (h hubDest) Deliver(ctx context.Context, batch []Envelope) ([]string, error) {
	var taken []string
	for _, e := range batch {
		source, fp, ev := e.HubEvent()
		if err := h.send(ctx, source, fp, ev); err != nil {
			return taken, err
		}
		taken = append(taken, e.Fingerprint)
	}
	return taken, nil
}

// InboxFeed says where the person's inbox takes signals (POST {origin}/api/ingest) and with which bearer token, read
// at the moment of delivery; ok false while no inbox is installed.
type InboxFeed func() (ingestURL, token string, ok bool)

// Inbox is the destination of the person's inbox (harnessly-inbox POST /api/ingest): the envelopes of connectors
// whose Spec names an inbox kind, as signals of the same shape its collectors send, with no excerpt.
func Inbox(feed InboxFeed, client *http.Client) Destination {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return inboxDest{feed, client}
}

type inboxDest struct {
	feed   InboxFeed
	client *http.Client
}

func (inboxDest) Name() string { return "inbox" }

func (inboxDest) Wants(e Envelope) bool {
	c, ok := Lookup(e.Connector)
	return ok && c.Spec().Inbox != ""
}

type ingestBody struct {
	Source struct {
		Kind  string `json:"kind"`
		Label string `json:"label"`
	} `json:"source"`
	Signals []InboxSignal `json:"signals"`
}

func (d inboxDest) Deliver(ctx context.Context, batch []Envelope) ([]string, error) {
	u, token, ok := d.feed()
	if !ok {
		return nil, fmt.Errorf("no inbox installed")
	}
	// One request per source of the inbox: a connection's kind and account.
	type key struct{ kind, label string }
	groups := map[key][]Envelope{}
	var order []key
	for _, e := range batch {
		c, ok := Lookup(e.Connector)
		if !ok {
			continue
		}
		k := key{c.Spec().Inbox, e.Account}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}
	var taken []string
	for _, k := range order {
		var body ingestBody
		body.Source.Kind, body.Source.Label = k.kind, k.label
		for _, e := range groups[k] {
			body.Signals = append(body.Signals, e.Signal())
		}
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
		if err != nil {
			return taken, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := d.client.Do(req)
		if err != nil {
			return taken, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		switch {
		case res.StatusCode >= 200 && res.StatusCode < 300:
			for _, e := range groups[k] {
				taken = append(taken, e.Fingerprint)
			}
		case res.StatusCode == http.StatusBadRequest:
			// The inbox does not take this kind (an older inbox): the envelopes are dropped for it, not retried forever.
			for _, e := range groups[k] {
				taken = append(taken, e.Fingerprint)
			}
		default:
			return taken, fmt.Errorf("the inbox answered %d", res.StatusCode)
		}
	}
	return taken, nil
}
