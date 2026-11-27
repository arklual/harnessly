package connect

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// codeTTL is how long a pairing code stays good.
const codeTTL = 30 * time.Minute

// DeviceRec is a paired device as the runtime keeps it.
type DeviceRec struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	Pub      []byte    `json:"pub"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen,omitempty"`
	Version  string    `json:"version,omitempty"`
	Online   bool      `json:"online"`
	Running  int       `json:"running"`
}

// Server is the runtime's side.
type Server struct {
	Dir  string // where devices.json lives
	Logf func(format string, a ...any)

	once     sync.Once
	mu       sync.Mutex
	devices  map[string]*DeviceRec
	codes    map[string]time.Time
	nonces   map[string]time.Time
	links    map[string]*srvLink
	sessions map[uint64]*session
	nextSID  atomic.Uint64
}

type srvLink struct {
	dev  string
	ws   *websocket.Conn
	out  chan outFrame
	done chan struct{}
	once sync.Once
}

func (l *srvLink) close() { l.once.Do(func() { close(l.done) }) }

func (l *srvLink) sendMsg(m Msg) bool {
	b, _ := json.Marshal(m)
	select {
	case l.out <- outFrame{b: b}:
		return true
	case <-l.done:
		return false
	}
}

// session is one command between its wrapper and its device; it outlives the device's link.
type session struct {
	id      uint64
	dev     string
	wrapper chan outFrame
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	got     [2]int64
	pending []Msg // signals and stdin that came while the device was away
}

func (s *session) finish() { s.once.Do(func() { close(s.done) }) }

func (s *session) toWrapper(f outFrame) {
	select {
	case s.wrapper <- f:
	case <-s.done:
	}
}

func (s *session) toWrapperMsg(m Msg) {
	b, _ := json.Marshal(m)
	s.toWrapper(outFrame{b: b})
}

func (s *Server) logf(format string, a ...any) {
	if s.Logf != nil {
		s.Logf(format, a...)
	}
}

func (s *Server) init() {
	s.once.Do(func() {
		s.devices = map[string]*DeviceRec{}
		s.codes = map[string]time.Time{}
		s.nonces = map[string]time.Time{}
		s.links = map[string]*srvLink{}
		s.sessions = map[uint64]*session{}
		s.nextSID.Store(uint64(time.Now().UnixNano()))
		raw, err := os.ReadFile(filepath.Join(s.Dir, "devices.json"))
		if err == nil {
			var list []*DeviceRec
			if json.Unmarshal(raw, &list) == nil {
				for _, d := range list {
					s.devices[d.ID] = d
				}
			}
		}
	})
}

// save writes the devices; s.mu is held.
func (s *Server) save() {
	list := make([]*DeviceRec, 0, len(s.devices))
	for _, d := range s.devices {
		c := *d
		c.Online, c.Running = false, 0
		list = append(list, &c)
	}
	slices.SortFunc(list, func(a, b *DeviceRec) int { return a.Created.Compare(b.Created) })
	raw, _ := json.MarshalIndent(list, "", "  ")
	_ = os.MkdirAll(s.Dir, 0o700)
	tmp := filepath.Join(s.Dir, ".devices.json.tmp")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(s.Dir, "devices.json"))
	}
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.ToLower(b32.EncodeToString(b))
}

// IssueCode gives a one-time pairing code, good for 30 minutes.
func (s *Server) IssueCode() (string, time.Time) {
	s.init()
	code := "hx_" + randomID(15)[:24]
	exp := time.Now().Add(codeTTL)
	s.mu.Lock()
	for c, e := range s.codes {
		if time.Now().After(e) {
			delete(s.codes, c)
		}
	}
	s.codes[code] = exp
	s.mu.Unlock()
	return code, exp
}

// Devices lists the paired devices, online first.
func (s *Server) Devices() []DeviceRec {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeviceRec, 0, len(s.devices))
	for _, d := range s.devices {
		c := *d
		_, c.Online = s.links[d.ID]
		for _, ss := range s.sessions {
			if ss.dev == d.ID {
				c.Running++
			}
		}
		c.Pub = nil
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b DeviceRec) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		return a.Created.Compare(b.Created)
	})
	return out
}

// Revoke forgets a device: its link is closed and it cannot dial again.
func (s *Server) Revoke(idOrName string) bool {
	s.init()
	s.mu.Lock()
	d := s.find(idOrName)
	if d == nil {
		s.mu.Unlock()
		return false
	}
	delete(s.devices, d.ID)
	l := s.links[d.ID]
	delete(s.links, d.ID)
	s.save()
	s.mu.Unlock()
	if l != nil {
		l.ws.Close(statusRevoked, "revoked")
		l.close()
	}
	return true
}

// find is a device by id or by name (any case); s.mu is held.
func (s *Server) find(idOrName string) *DeviceRec {
	if d := s.devices[idOrName]; d != nil {
		return d
	}
	for _, d := range s.devices {
		if strings.EqualFold(d.Name, idOrName) {
			return d
		}
	}
	return nil
}

type registerReq struct {
	Code string `json:"code"`
	Name string `json:"name"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Pub  []byte `json:"pub"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeRegister is POST /connect/v1/register: a pairing code and the device's public key make a device.
func (s *Server) ServeRegister(w http.ResponseWriter, r *http.Request) {
	s.init()
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var req registerReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	if len(req.Pub) != ed25519.PublicKeySize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad public key"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 80 {
		name = "device"
	}
	s.mu.Lock()
	exp, ok := s.codes[req.Code]
	if !ok || time.Now().After(exp) {
		s.mu.Unlock()
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "код неверный или истёк: возьмите новый"})
		return
	}
	delete(s.codes, req.Code)
	d := &DeviceRec{ID: "d-" + randomID(10), Name: name, OS: req.OS, Arch: req.Arch, Pub: req.Pub, Created: time.Now().UTC()}
	s.devices[d.ID] = d
	s.save()
	s.mu.Unlock()
	s.logf("connect: paired %s (%s, %s/%s)", d.ID, d.Name, d.OS, d.Arch)
	writeJSON(w, http.StatusOK, map[string]string{"id": d.ID, "name": d.Name})
}

// ServeDial is GET /connect/v1/dial: a device's link.
func (s *Server) ServeDial(w http.ResponseWriter, r *http.Request) {
	s.init()
	a, err := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if p := r.Header.Get("X-Connect-Proto"); p != strconv.Itoa(Protocol) {
		http.Error(w, "protocol "+strconv.Itoa(Protocol)+" required", http.StatusUpgradeRequired)
		return
	}
	s.mu.Lock()
	d := s.devices[a.MachineID]
	var pub ed25519.PublicKey
	if d != nil {
		pub = d.Pub
	}
	_, replay := s.nonces[a.Nonce]
	s.mu.Unlock()
	if d == nil {
		http.Error(w, "unknown or revoked device", http.StatusForbidden)
		return
	}
	if replay || hubproto.VerifyDial(pub, a, strings.ToLower(r.Host), time.Now()) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	now := time.Now()
	for n, t := range s.nonces {
		if now.Sub(t) > hubproto.NonceMemory {
			delete(s.nonces, n)
		}
	}
	s.nonces[a.Nonce] = now
	s.mu.Unlock()

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 20)
	l := &srvLink{dev: d.ID, ws: ws, out: make(chan outFrame, 256), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go linkWriter(ctx, ws, l.out, l.done)
	go linkPinger(ctx, ws, l.done)
	err = s.serveLink(ctx, l)
	l.close()
	ws.CloseNow()
	s.mu.Lock()
	if s.links[d.ID] == l {
		delete(s.links, d.ID)
		if rec := s.devices[d.ID]; rec != nil {
			rec.LastSeen = time.Now().UTC()
		}
	}
	s.mu.Unlock()
	s.logf("connect: %s offline (%v)", d.ID, err)
}

func linkWriter(ctx context.Context, ws *websocket.Conn, out chan outFrame, done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case f := <-out:
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			typ := websocket.MessageText
			if f.binary {
				typ = websocket.MessageBinary
			}
			err := ws.Write(wctx, typ, f.b)
			cancel()
			if err != nil {
				ws.CloseNow()
				return
			}
		}
	}
}

func linkPinger(ctx context.Context, ws *websocket.Conn, done chan struct{}) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := ws.Ping(pctx)
			cancel()
			if err != nil {
				ws.CloseNow()
				return
			}
		}
	}
}

func (s *Server) serveLink(ctx context.Context, l *srvLink) error {
	// The first frame is the device's hello.
	typ, b, err := l.ws.Read(ctx)
	if err != nil {
		return err
	}
	var hello Msg
	if typ != websocket.MessageText || json.Unmarshal(b, &hello) != nil || hello.T != "hello" {
		return errors.New("no hello")
	}
	s.mu.Lock()
	old := s.links[l.dev]
	s.links[l.dev] = l
	if rec := s.devices[l.dev]; rec != nil {
		rec.OS, rec.Arch, rec.Version, rec.LastSeen = hello.OS, hello.Arch, hello.Version, time.Now().UTC()
		s.save()
	}
	var resume, lost []*session
	for _, ss := range s.sessions {
		if ss.dev != l.dev {
			continue
		}
		if slices.Contains(hello.SIDs, strconv.FormatUint(ss.id, 10)) {
			resume = append(resume, ss)
		} else {
			lost = append(lost, ss)
		}
	}
	s.mu.Unlock()
	if old != nil {
		old.close()
		old.ws.CloseNow()
	}
	s.logf("connect: %s online (%s/%s %s), %d to resume", l.dev, hello.OS, hello.Arch, hello.Version, len(resume))
	for _, ss := range lost {
		s.fail(ss, "команда потеряна: Connect на устройстве перезапускался")
	}
	for _, ss := range resume {
		ss.mu.Lock()
		got, pending := ss.got, ss.pending
		ss.pending = nil
		ss.mu.Unlock()
		l.sendMsg(Msg{T: "attach", SID: strconv.FormatUint(ss.id, 10), Out: got[0], Err: got[1]})
		for _, m := range pending {
			l.sendMsg(m)
		}
	}
	for {
		typ, b, err := l.ws.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			kind, sid, off, payload, err := DecodeData(b)
			if err != nil || (kind != KindStdout && kind != KindStderr) {
				continue
			}
			if ss := s.session(sid); ss != nil && ss.dev == l.dev {
				ss.data(kind, off, payload)
			}
			continue
		}
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		id, _ := strconv.ParseUint(m.SID, 10, 64)
		ss := s.session(id)
		if ss == nil || ss.dev != l.dev {
			if m.T == "exit" {
				l.sendMsg(Msg{T: "release", SID: m.SID})
			}
			continue
		}
		switch m.T {
		case "exit":
			ss.toWrapperMsg(Msg{T: "exit", Code: m.Code, Files: m.Files})
			s.drop(ss)
			l.sendMsg(Msg{T: "release", SID: m.SID})
		case "gone":
			s.fail(ss, "команда потеряна на устройстве")
		}
	}
}

// data passes on the part of a chunk the wrapper has not had yet.
func (ss *session) data(kind byte, off int64, payload []byte) {
	i := int(kind - 1)
	ss.mu.Lock()
	got := ss.got[i]
	var gap int64
	switch {
	case off > got:
		gap = off - got
	case off+int64(len(payload)) <= got:
		ss.mu.Unlock()
		return
	default:
		payload = payload[got-off:]
		off = got
	}
	ss.got[i] = off + int64(len(payload))
	ss.mu.Unlock()
	if gap > 0 {
		note := "\n[harnessly connect: пропущено " + strconv.FormatInt(gap, 10) + " байт вывода]\n"
		ss.toWrapper(outFrame{binary: true, b: append([]byte{KindStderr}, note...)})
	}
	ss.toWrapper(outFrame{binary: true, b: append([]byte{kind}, payload...)})
}

func (s *Server) session(id uint64) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) drop(ss *session) {
	s.mu.Lock()
	delete(s.sessions, ss.id)
	s.mu.Unlock()
}

// fail ends a session with a message for the wrapper.
func (s *Server) fail(ss *session, msg string) {
	ss.toWrapperMsg(Msg{T: "error", Error: msg})
	s.drop(ss)
}

func (s *Server) link(dev string) *srvLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.links[dev]
}

// toDevice sends a message for a session, or keeps it for the device's return.
func (s *Server) toDevice(ss *session, m Msg) {
	if l := s.link(ss.dev); l != nil && l.sendMsg(m) {
		return
	}
	ss.mu.Lock()
	ss.pending = append(ss.pending, m)
	ss.mu.Unlock()
}

// ServeExec is the wrapper's socket (the caller checks the cli-token): one command, start to exit.
func (s *Server) ServeExec(w http.ResponseWriter, r *http.Request) {
	s.init()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	refuse := func(msg string) {
		b, _ := json.Marshal(Msg{T: "error", Error: msg})
		_ = ws.Write(ctx, websocket.MessageText, b)
		ws.Close(websocket.StatusNormalClosure, "")
	}
	typ, b, err := ws.Read(ctx)
	var m Msg
	if err != nil || typ != websocket.MessageText || json.Unmarshal(b, &m) != nil || m.T != "exec" {
		refuse("ожидался exec")
		return
	}
	s.mu.Lock()
	d := s.find(m.Device)
	var l *srvLink
	if d != nil {
		l = s.links[d.ID]
	}
	s.mu.Unlock()
	switch {
	case d == nil:
		refuse("устройство " + strconv.Quote(m.Device) + " не подключено к harnessly")
		return
	case l == nil:
		refuse("устройство «" + d.Name + "» офлайн: откройте Harnessly Connect на нём")
		return
	}
	ss := &session{id: s.nextSID.Add(1), dev: d.ID, wrapper: make(chan outFrame, 256), done: make(chan struct{})}
	s.mu.Lock()
	s.sessions[ss.id] = ss
	s.mu.Unlock()
	sid := strconv.FormatUint(ss.id, 10)
	m.SID, m.Device = sid, ""
	if !l.sendMsg(m) {
		s.drop(ss)
		refuse("устройство «" + d.Name + "» отключилось")
		return
	}
	// The wrapper's own frames: stdin and signals. Its socket closing first kills the command.
	go func() {
		defer cancel()
		for {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var in Msg
			if typ != websocket.MessageText || json.Unmarshal(b, &in) != nil {
				continue
			}
			switch in.T {
			case "stdin", "signal":
				in.SID = sid
				s.toDevice(ss, in)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			if s.session(ss.id) != nil {
				s.drop(ss)
				s.toDevice(ss, Msg{T: "signal", SID: sid, Sig: "KILL"})
				if l := s.link(ss.dev); l != nil {
					l.sendMsg(Msg{T: "release", SID: sid})
				}
			}
			ss.finish()
			return
		case f := <-ss.wrapper:
			typ := websocket.MessageText
			if f.binary {
				typ = websocket.MessageBinary
			}
			if ws.Write(ctx, typ, f.b) != nil {
				cancel()
				continue
			}
			if !f.binary {
				var out Msg
				if json.Unmarshal(f.b, &out) == nil && (out.T == "exit" || out.T == "error") {
					ss.finish()
					ws.Close(websocket.StatusNormalClosure, "")
					return
				}
			}
		}
	}
}
