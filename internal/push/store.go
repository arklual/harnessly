package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"harnessly/internal/jsvalue"
)

// SubscriptionsFile is the browsers' push subscriptions in the data folder.
const SubscriptionsFile = "push-subscriptions.json"

// VisibilityTTL: a device that has not said it is visible for this long counts as not looked at.
const VisibilityTTL = 60 * time.Second

// Subscription is one browser's push subscription, with the device that made it.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	DeviceID string `json:"deviceId,omitempty"`
	Mobile   *bool  `json:"mobile,omitempty"`
}

// Store reads and edits the subscriptions file and remembers which devices are on screen. Every writer of this process
// goes through one Store and its lock (the sender dropping a gone subscription, the page's subscribe route); a writer
// in another process (the previous runtime's routes, while their slice is off) replaces the file whole, as these do.
type Store struct {
	Dir string
	Now func() time.Time

	mu      sync.Mutex
	visible map[string]time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) path() string { return filepath.Join(s.Dir, SubscriptionsFile) }

func (s *Store) read() []Subscription {
	raw, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]Subscription, 0, len(items))
	for _, it := range items {
		var sub Subscription
		if json.Unmarshal(it, &sub) != nil {
			continue
		}
		out = append(out, sub)
	}
	return out
}

// Subscriptions are every stored subscription; none when the file is missing or unreadable.
func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Remove drops the subscriptions of an endpoint (one the push service said is gone, or the page unsubscribed); the
// others are written back as they were.
func (s *Store) Remove(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.items()
	kept := make([]any, 0, len(items))
	for _, it := range items {
		if e, ok := endpointOf(it); !ok || e != endpoint {
			kept = append(kept, it)
		}
	}
	if len(kept) == len(items) {
		return nil
	}
	return s.write(kept)
}

// items is the file as the page's writer left it, every record with its keys in their order.
func (s *Store) items() []any {
	raw, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	items, _ := v.([]any)
	return items
}

func endpointOf(item any) (any, bool) {
	o, ok := item.(*jsvalue.Object)
	if !ok {
		return nil, false
	}
	return o.Get("endpoint")
}

// write replaces the file, two-space indented like the page's writer (0600, through a temporary file).
func (s *Store) write(items []any) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if items == nil {
		items = []any{}
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, jsvalue.Stringify(items, "  "), 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Add stores a browser's subscription ({endpoint, keys} as the browser gave them) with the device that made it. A
// device subscribing again replaces its earlier subscription; an unchanged record is not written again (every page
// load registers).
func (s *Store) Add(endpoint string, keys any, deviceID string, mobile *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := jsvalue.NewObject("endpoint", endpoint, "keys", keys)
	if deviceID != "" {
		stored.Set("deviceId", deviceID)
	}
	if mobile != nil {
		stored.Set("mobile", *mobile)
	}
	current := s.items()
	for _, it := range current {
		if jsvalue.Equal(it, stored) {
			return nil
		}
	}
	subs := make([]any, 0, len(current)+1)
	for _, it := range current {
		o, _ := it.(*jsvalue.Object)
		if e, ok := endpointOf(it); ok && e == endpoint {
			continue
		}
		if deviceID != "" && o != nil {
			if d, ok := o.Get("deviceId"); ok && d == deviceID {
				continue
			}
		}
		subs = append(subs, it)
	}
	return s.write(append(subs, stored))
}

// DeviceEndpoints is {endpoint} of every stored subscription, in the file's order.
func (s *Store) DeviceEndpoints() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.items()
	out := make([]any, 0, len(items))
	for _, it := range items {
		o := jsvalue.NewObject()
		if e, ok := endpointOf(it); ok {
			o.Set("endpoint", e)
		}
		out = append(out, o)
	}
	return out
}

// MarkVisible: the device shows the app now.
func (s *Store) MarkVisible(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.visible == nil {
		s.visible = map[string]time.Time{}
	}
	s.visible[deviceID] = s.now()
}

// MarkHidden: the device hid the app.
func (s *Store) MarkHidden(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.visible, deviceID)
}

// Visible are the devices seen on screen within VisibilityTTL.
func (s *Store) Visible() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := map[string]bool{}
	for id, seen := range s.visible {
		if now.Sub(seen) > VisibilityTTL {
			delete(s.visible, id)
			continue
		}
		out[id] = true
	}
	return out
}

// SelectTargets: while someone has the app open on a device, only that device is notified (its page turns the push into
// a toast and the phone in the pocket stays quiet); nobody looking, every device is.
func SelectTargets(subs []Subscription, visible map[string]bool) []Subscription {
	if len(visible) == 0 {
		return subs
	}
	var watching []Subscription
	for _, s := range subs {
		if s.DeviceID != "" && visible[s.DeviceID] {
			watching = append(watching, s)
		}
	}
	if len(watching) > 0 {
		return watching
	}
	return subs
}
