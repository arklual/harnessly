package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// An action outward on the person's behalf (send a letter, a message, create a meeting) never runs on an agent's word.
// The agent proposes it; the runtime keeps the proposal with exactly its arguments and shows it as a card in the chat
// that asked; only the person's yes on that card (a page request with the person's session, which no agent holds) runs
// it, with the same arguments, and every run is a line of the journal of sends (dir/sent.jsonl). Nothing an agent sends
// can approve, widen or skip this: there is no route for it.

// Proposal states.
const (
	Waiting  = "waiting"
	Done     = "done"
	Failed   = "failed"
	Declined = "declined"
	Expired  = "expired"
)

// proposalTTL is how long a proposal waits for the person.
const proposalTTL = 30 * time.Minute

// By is who asks: the agent of a chat.
type By struct {
	Conv  string `json:"conv,omitempty"`
	Title string `json:"title,omitempty"`
}

// Pair is one argument as the card shows it, in the action's order.
type Pair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Proposal is an action waiting for the person's yes, as the card and the agent see it.
type Proposal struct {
	ID         string    `json:"id"`
	Connection string    `json:"connection"`
	Connector  string    `json:"connector"`
	Name       string    `json:"name"`
	Action     string    `json:"action"`
	Title      Text      `json:"title"`
	Args       []Pair    `json:"args"`
	Why        string    `json:"why,omitempty"`
	Conv       string    `json:"conv,omitempty"`
	State      string    `json:"state"`
	Result     string    `json:"result,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type pending struct {
	p    Proposal
	args map[string]string
	done chan struct{}
}

// Sent is a line of the journal of sends.
type Sent struct {
	At         time.Time `json:"at"`
	Proposal   string    `json:"proposal"`
	Connection string    `json:"connection"`
	Connector  string    `json:"connector"`
	Action     string    `json:"action"`
	Args       []Pair    `json:"args"`
	Why        string    `json:"why,omitempty"`
	// Conv is the chat whose agent proposed it; ApprovedBy who said yes: "person" on the page, "relay:<connection>" in
	// a bound chat of a Relay, "direct" for an Action.Direct that needs no yes.
	Conv       string `json:"conv,omitempty"`
	ApprovedBy string `json:"approvedBy"`
	OK         bool   `json:"ok"`
	Result     string `json:"result,omitempty"`
}

// ErrNotWaiting: the proposal was answered, ran out of time or never was.
var ErrNotWaiting = errors.New("not-waiting")

// Propose keeps an action for the person's yes. It never runs it.
func (s *Service) Propose(id, action string, args map[string]string, why string, by By) (Proposal, error) {
	conn, c, err := s.open(id)
	if err != nil {
		return Proposal{}, err
	}
	a, ok := actionOf(c.Spec(), action)
	if !ok {
		return Proposal{}, ErrUnknownTool
	}
	if g, ok := c.(ActGate); ok {
		if err := g.CanAct(s.instance(conn, nil), action); err != nil {
			return Proposal{}, err
		}
	}
	kept := map[string]string{}
	var pairs []Pair
	known := map[string]bool{}
	for _, arg := range a.Args {
		known[arg.Name] = true
		v := args[arg.Name]
		if v == "" && arg.Required {
			return Proposal{}, Sayf("%s: required", arg.Name)
		}
		if v != "" {
			kept[arg.Name] = v
			pairs = append(pairs, Pair{Name: arg.Name, Value: v})
		}
	}
	for k := range args {
		if !known[k] {
			return Proposal{}, Sayf("%s: not an argument of %s", k, action)
		}
	}
	p := Proposal{ID: newID("ca"), Connection: conn.ID, Connector: conn.Connector, Name: conn.Name, Action: action,
		Title: a.Title, Args: pairs, Why: oneLine(why), Conv: by.Conv, State: Waiting, CreatedAt: s.now().UTC()}
	if a.Direct {
		return s.direct(p, kept), nil
	}
	s.mu.Lock()
	s.expireLocked()
	s.pending[p.ID] = &pending{p: p, args: kept, done: make(chan struct{})}
	s.mu.Unlock()
	s.changed()
	s.relay(p, false)
	return p, nil
}

// direct runs an Action.Direct at once (it reaches only the person themselves) and writes the journal of sends.
func (s *Service) direct(p Proposal, args map[string]string) Proposal {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := s.act(ctx, p, args)
	if err != nil {
		p.State, p.Result = Failed, sayOf(err)
	} else {
		p.State, p.Result = Done, res
	}
	line := Sent{At: s.now().UTC(), Proposal: p.ID, Connection: p.Connection, Connector: p.Connector, Action: p.Action,
		Args: p.Args, Why: p.Why, Conv: p.Conv, ApprovedBy: "direct", OK: err == nil, Result: p.Result}
	if jerr := s.journal(line); jerr != nil {
		s.runner.log.Warn("journal of sends not written", "proposal", p.ID, "error", jerr)
	}
	return p
}

// relay shows a new proposal (settle: closes an answered one) in every working connection of a Relay connector.
func (s *Service) relay(p Proposal, settle bool) {
	list, err := s.List()
	if err != nil {
		return
	}
	for _, c := range list {
		conn, ok := Lookup(c.Connector)
		if !ok || c.State != StateOK {
			continue
		}
		r, ok := conn.(Relay)
		if !ok {
			continue
		}
		in := s.instance(c, nil)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var err error
			if settle {
				err = r.Settle(ctx, in, p)
			} else {
				err = r.Offer(ctx, in, p)
			}
			if err != nil {
				s.runner.log.Warn("proposal not relayed", "connection", c.ID, "proposal", p.ID, "error", sayOf(err))
			}
		}()
	}
}

// expireLocked closes the proposals that waited too long; s.mu is held.
func (s *Service) expireLocked() {
	now := s.now()
	for id, w := range s.pending {
		if w.p.State == Waiting && now.Sub(w.p.CreatedAt) > proposalTTL {
			w.p.State = Expired
			close(w.done)
		}
		if w.p.State != Waiting && now.Sub(w.p.CreatedAt) > 2*proposalTTL {
			delete(s.pending, id)
		}
	}
}

// Proposals are the actions that wait for the person, of every chat.
func (s *Service) Proposals() []Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	out := []Proposal{}
	for _, w := range s.pending {
		if w.p.State == Waiting {
			out = append(out, w.p)
		}
	}
	return out
}

// Wait answers the proposal as soon as it is not waiting, or after d.
func (s *Service) Wait(ctx context.Context, id string, d time.Duration) (Proposal, error) {
	s.mu.Lock()
	s.expireLocked()
	w := s.pending[id]
	s.mu.Unlock()
	if w == nil {
		return Proposal{}, ErrNotWaiting
	}
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-w.done:
		case <-t.C:
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return w.p, nil
}

// Answer is the person's yes or no to a proposal. A yes runs the action with exactly the kept arguments and writes the
// journal of sends. Only the page calls it, with the person's session.
func (s *Service) Answer(ctx context.Context, id string, approve bool) (Proposal, error) {
	return s.answer(ctx, id, approve, "person")
}

// answer is Answer as given by: "person" on the page, "relay:<connection>" in a Relay's bound chat.
func (s *Service) answer(ctx context.Context, id string, approve bool, by string) (Proposal, error) {
	s.mu.Lock()
	s.expireLocked()
	w := s.pending[id]
	if w == nil || w.p.State != Waiting {
		s.mu.Unlock()
		return Proposal{}, ErrNotWaiting
	}
	if !approve {
		w.p.State = Declined
		close(w.done)
		p := w.p
		s.mu.Unlock()
		s.changed()
		s.relay(p, true)
		return p, nil
	}
	// Taken out of waiting before it runs: a second yes cannot run it twice.
	w.p.State = "running"
	p, args := w.p, w.args
	s.mu.Unlock()

	res, err := s.act(ctx, p, args)
	s.mu.Lock()
	if err != nil {
		w.p.State, w.p.Result = Failed, sayOf(err)
	} else {
		w.p.State, w.p.Result = Done, res
	}
	close(w.done)
	p = w.p
	s.mu.Unlock()
	line := Sent{At: s.now().UTC(), Proposal: p.ID, Connection: p.Connection, Connector: p.Connector, Action: p.Action,
		Args: p.Args, Why: p.Why, Conv: p.Conv, ApprovedBy: by, OK: err == nil, Result: p.Result}
	if jerr := s.journal(line); jerr != nil {
		s.runner.log.Warn("journal of sends not written", "proposal", p.ID, "error", jerr)
	}
	s.changed()
	s.relay(p, true)
	return p, nil
}

func (s *Service) act(ctx context.Context, p Proposal, args map[string]string) (string, error) {
	conn, c, err := s.open(p.Connection)
	if err != nil {
		return "", err
	}
	a, ok := c.(Actor)
	if !ok {
		return "", ErrUnknownTool
	}
	in := s.instance(conn, nil)
	in.Conv = p.Conv
	res, err := a.Act(ctx, in, p.Action, args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}

func (s *Service) journal(line Sent) error {
	raw, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "sent.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

// Journal is the journal of sends, the newest first, at most limit lines.
func (s *Service) Journal(limit int) ([]Sent, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, "sent.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []Sent{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	out := []Sent{}
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		var l Sent
		if json.Unmarshal([]byte(lines[i]), &l) == nil {
			out = append(out, l)
		}
	}
	return out, nil
}
