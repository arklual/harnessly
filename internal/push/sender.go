// Package push sends the web push notifications of the runtime: a finished turn or a question of an agent, to the
// person's subscribed browsers (RFC 8030 delivery, RFC 8291 message encryption, RFC 8292 VAPID). The subscriptions and
// the key pair are files in the data folder, shared with the page's subscribe route.
package push

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/status"
)

// TTL is how long a push service keeps an undelivered notification.
const TTL = 6 * time.Hour

// Config is what the sender needs; the deployment names the product parts.
type Config struct {
	// Dir is the data folder holding the subscriptions and the key pair.
	Dir string
	// Subject is the VAPID subject: a reachable https: or mailto: URL (Apple refuses others).
	Subject string
	// TagPrefix starts a notification's tag (prefix + tab id): a newer push of a conversation replaces the older.
	TagPrefix string
	Client    *http.Client
	Now       func() time.Time
	Log       func(level, msg string, kv ...any)
}

// Sender sends pushes and implements status.Pusher.
type Sender struct {
	cfg   Config
	Store *Store

	mu     sync.Mutex
	signer *signer
}

// New sends from cfg.Dir.
func New(cfg Config) *Sender {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Sender{cfg: cfg, Store: &Store{Dir: cfg.Dir, Now: cfg.Now}}
}

func (s *Sender) log(level, msg string, kv ...any) {
	if s.cfg.Log != nil {
		s.cfg.Log(level, msg, kv...)
	}
}

// TagPrefix starts the tags of this deployment's notifications.
func (s *Sender) TagPrefix() string { return s.cfg.TagPrefix }

// PublicKey is the VAPID public key the page subscribes with (created with the pair when missing).
func (s *Sender) PublicKey() (string, error) {
	sg, err := s.keys()
	if err != nil {
		return "", err
	}
	return sg.public, nil
}

func (s *Sender) keys() (*signer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signer != nil {
		return s.signer, nil
	}
	k, err := LoadOrCreateVAPID(s.cfg.Dir)
	if err != nil {
		return nil, err
	}
	sg, err := k.signer()
	if err != nil {
		return nil, err
	}
	s.signer = sg
	return sg, nil
}

// Topic is the Topic header of a tag: a hex digest, as Apple rejects "-" and "_" although RFC 8030 allows them.
func Topic(tag string) string {
	sum := sha256.Sum256([]byte(tag))
	return hex.EncodeToString(sum[:])[:32]
}

// Send delivers one message and returns the push service's HTTP status (0 when it could not be reached), so the
// caller can drop a subscription that is gone.
func (s *Sender) Send(sub Subscription, msg Message) (int, error) {
	sg, err := s.keys()
	if err != nil {
		return 0, err
	}
	uaPublic, err1 := b64.DecodeString(sub.Keys.P256dh)
	auth, err2 := b64.DecodeString(sub.Keys.Auth)
	if err1 != nil || err2 != nil {
		uaPublic, err1 = decodeAnyBase64(sub.Keys.P256dh)
		auth, err2 = decodeAnyBase64(sub.Keys.Auth)
		if err1 != nil || err2 != nil {
			return 0, errors.New("push: bad subscription keys")
		}
	}
	body, err := Encrypt(nodejson.Marshal(msg), uaPublic, auth)
	if err != nil {
		return 0, err
	}
	authz, err := sg.authorization(sub.Endpoint, s.cfg.Subject, s.cfg.Now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	urgency := "normal"
	if msg.Kind == "question" {
		urgency = "high"
	}
	req.Header.Set("TTL", strconv.Itoa(int(TTL/time.Second)))
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Topic", Topic(msg.Tag))
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", authz)
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		host := ""
		if u, err := url.Parse(sub.Endpoint); err == nil {
			host = u.Host
		}
		s.log("warn", "web push rejected", "status", resp.StatusCode, "host", host, "body", string(snippet))
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

func decodeAnyBase64(v string) ([]byte, error) {
	for _, enc := range []interface{ DecodeString(string) ([]byte, error) }{b64, urlPadded, stdPadded, stdRaw} {
		if b, err := enc.DecodeString(v); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64")
}

// Push sends a turn's notification ("done" or "question") to the devices it is for, dropping subscriptions the push
// service says are gone (404, 410). It implements status.Pusher.
func (s *Sender) Push(tabID string, e status.Entry, kind string) {
	subs := s.Store.Subscriptions()
	if len(subs) == 0 {
		return
	}
	msg := BuildMessage(tabID, e, kind, s.cfg.TagPrefix, s.cfg.Now())
	for _, sub := range SelectTargets(subs, s.Store.Visible()) {
		code, err := s.Send(sub, msg)
		if err != nil {
			s.log("warn", "web push failed", "err", err.Error())
		}
		if code == http.StatusNotFound || code == http.StatusGone {
			if err := s.Store.Remove(sub.Endpoint); err != nil {
				s.log("warn", "push subscription not removed", "err", err.Error())
			}
		}
	}
}

var _ status.Pusher = (*Sender)(nil)
