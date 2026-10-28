// Package sshinstall is the hub's «Свой сервер по SSH» way of connecting a machine. The owner adds the public key of
// their space to the server's authorized_keys and gives the hub an address; the hub connects once, looks at the server,
// runs the same install command a person would paste, waits for the machine to come online, and forgets the session
// (docs/hub/architecture.md, "Ways to connect").
//
// Every space has an SSH key pair of its own, made on first use: the owners of that space are the only people who are
// shown the public half, and the private half (encrypted in the database, with a key the hub keeps outside it) signs in
// only for an install an owner of that space starts. A key shared by all the spaces would let the owner of one install
// on the servers of another.
//
// The hub has never seen the server, so its host key is the owner's to confirm: the first request (no
// hostKeyFingerprint) goes as far as the handshake, shows the fingerprint and stops before any key is offered; the same
// request again with that fingerprint carries on, and a server whose key is another one is refused.
//
// The progress is a job of kind ssh-install on the space's event stream, for the owners: steps ssh, preflight, install,
// online.
package sshinstall

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	dialTimeout    = 15 * time.Second
	handshakeLimit = 20 * time.Second
	preflightLimit = 30 * time.Second
	installLimit   = 6 * time.Minute
	onlineLimit    = 90 * time.Second
	// maxOutput bounds what is read from the server per command.
	maxOutput = 256 << 10
	// tailLines is how many lines of a failed install go to the page.
	tailLines = 8
	maxJobs   = 8
	perHour   = 10
	// probesPerHour bounds the first requests of an install (they only look at the server's host key).
	probesPerHour = 30
	pollEvery     = 500 * time.Millisecond
	jobKind       = "ssh-install"
	maxDetail     = 600
)

// Dialer opens the network connection to a server. Tests replace it.
type Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

// Deps are what the service is built from.
type Deps struct {
	Config   *config.Config
	DB       *store.DB
	Auth     *auth.Service
	Spaces   *spaces.Service
	Machines *machines.Service
	Reg      *link.Registry
	Events   *events.Broker
	Limiter  *ratelimit.Limiter
	Log      *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Dial opens connections; nil uses the network, which refuses addresses that are not on the public internet
	// (HUB_SSH_ALLOW_PRIVATE allows them, outside the cloud edition).
	Dial Dialer
	// OnlineWait is how long the last step waits for the machine; zero means 90 seconds. Tests shorten it.
	OnlineWait time.Duration
}

// Service runs install jobs over SSH.
type Service struct {
	cfg    *config.Config
	db     *store.DB
	auth   *auth.Service
	spaces *spaces.Service
	mach   *machines.Service
	reg    *link.Registry
	events *events.Broker
	lim    *ratelimit.Limiter
	log    *slog.Logger
	now    func() time.Time
	dial   Dialer
	online time.Duration

	master []byte
	keyMu  sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	slots  chan struct{}
	wg     sync.WaitGroup
}

// New makes the service and loads (or makes) the key that encrypts the spaces' SSH keys. A key that cannot be had is an
// error: the caller leaves the way off.
func New(d Deps) (*Service, error) {
	master, err := masterKey(d.Config.DataDir, d.Config.SecretKey)
	if err != nil {
		return nil, err
	}
	s := &Service{
		cfg: d.Config, db: d.DB, auth: d.Auth, spaces: d.Spaces, mach: d.Machines, reg: d.Reg, events: d.Events, lim: d.Limiter,
		log: d.Log, now: d.Now, dial: d.Dial, online: d.OnlineWait, master: master, slots: make(chan struct{}, maxJobs),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.lim == nil {
		s.lim = ratelimit.New()
	}
	if s.online == 0 {
		s.online = onlineLimit
	}
	if s.dial == nil {
		s.dial = networkDialer(d.Config.SSHAllowPrivate && d.Config.Edition != config.EditionCloud)
	}
	return s, nil
}

// Available says the way is on: it has a key and the setting allows it.
func (s *Service) Available() bool { return s != nil && s.cfg.SSH }

// signerFor is the SSH key pair of a space, made on first use. A stored key that cannot be decrypted (the hub's key
// changed: a data folder without its volume, another HUB_SECRET_KEY) is replaced by a new pair, and the owners are shown
// the new public half.
func (s *Service) signerFor(ctx context.Context, spaceID string) (ssh.Signer, string, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	st := s.db.S()
	k, err := st.SpaceSSHKeyBySpace(ctx, spaceID)
	if errors.Is(err, store.ErrNotFound) {
		signer, pub, enc, err := spaceKey(s.master, spaceID)
		if err != nil {
			return nil, "", err
		}
		made, err := st.CreateSpaceSSHKey(ctx, store.SpaceSSHKey{SpaceID: spaceID, PublicKey: pub, PrivateKeyEnc: enc, CreatedAt: s.now().UTC()})
		if err != nil {
			return nil, "", err
		}
		if made {
			return signer, pub, nil
		}
		// Another hub process made it a moment earlier.
		if k, err = st.SpaceSSHKeyBySpace(ctx, spaceID); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}
	signer, err := openSpaceKey(s.master, spaceID, k.PrivateKeyEnc)
	if err != nil {
		s.log.Warn("the SSH key of a space cannot be decrypted, a new one is made: the owners have to add its public half to their servers again", "space", spaceID, "error", err)
		signer, pub, enc, err := spaceKey(s.master, spaceID)
		if err != nil {
			return nil, "", err
		}
		if err := st.ReplaceSpaceSSHKey(ctx, store.SpaceSSHKey{SpaceID: spaceID, PublicKey: pub, PrivateKeyEnc: enc, CreatedAt: s.now().UTC()}); err != nil {
			return nil, "", err
		}
		return signer, pub, nil
	}
	return signer, publicLine(signer.PublicKey()), nil
}

// Close stops the jobs still running and waits for them.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

// Mount registers the endpoints on the router of the paths under a space.
func (s *Service) Mount(sub *http.ServeMux) {
	sub.Handle("GET /api/spaces/{spaceId}/machines/ssh", s.auth.Require(s.showKey))
	sub.Handle("POST /api/spaces/{spaceId}/machines/ssh", s.auth.Require(s.start))
}

// showKey answers the owners of a space with the public key to add to a server's authorized_keys: the space's own, which
// nobody else is shown.
func (s *Service) showKey(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	if err := s.spaces.Owner(ctx, p.User.ID, r.PathValue("spaceId")); err != nil {
		return err
	}
	if !s.Available() {
		return httpx.ErrProviderOff
	}
	_, pub, err := s.signerFor(ctx, r.PathValue("spaceId"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"publicKey": pub})
	return nil
}

var (
	hostName    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	userName    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,31}$`)
	fingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
)

type startRequest struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Profile     string `json:"profile"`
	ConfirmFull bool   `json:"confirmFull"`
	// HostKeyFingerprint is the SHA256 fingerprint of the server's host key that the owner has compared with the
	// server's own. Without it the job only looks at the host key and stops.
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
}

// target is a request that passed validation. fingerprint is empty on the first request of an install.
type target struct {
	host, user, profile, fingerprint string
	port                             int
}

func (t target) addr() string { return net.JoinHostPort(t.host, strconv.Itoa(t.port)) }

func (s *Service) check(req startRequest) (target, error) {
	t := target{host: strings.TrimSpace(req.Host), user: strings.TrimSpace(req.User), profile: req.Profile, port: req.Port, fingerprint: strings.TrimSpace(req.HostKeyFingerprint)}
	t.host = strings.TrimSuffix(strings.TrimPrefix(t.host, "["), "]")
	if t.port == 0 {
		t.port = 22
	}
	switch {
	case t.host == "" || (net.ParseIP(t.host) == nil && !hostName.MatchString(t.host)):
		return target{}, httpx.Invalid("host", "Укажите адрес сервера: имя или IP-адрес")
	case t.port < 1 || t.port > 65535:
		return target{}, httpx.Invalid("port", "Порт — число от 1 до 65535")
	case !userName.MatchString(t.user):
		return target{}, httpx.Invalid("user", "Укажите пользователя на сервере")
	case t.fingerprint != "" && !fingerprint.MatchString(t.fingerprint):
		return target{}, httpx.Invalid("hostKeyFingerprint", "Отпечаток ключа сервера выглядит как SHA256:… (43 знака после двоеточия)")
	}
	if err := s.mach.CheckProfile(t.profile, req.ConfirmFull); err != nil {
		return target{}, err
	}
	return t, nil
}

func (s *Service) start(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req startRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	if err := s.spaces.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	if !s.Available() {
		return httpx.ErrProviderOff
	}
	t, err := s.check(req)
	if err != nil {
		return err
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return httpx.Conflict("busy", "Хаб сейчас ставит несколько машин: подождите минуту")
	}
	// The first request of an install only looks at the host key; the one with the owner's fingerprint installs.
	probe := t.fingerprint == ""
	key, limit := "ssh:"+p.User.ID, perHour
	if probe {
		key, limit = "ssh-probe:"+p.User.ID, probesPerHour
	}
	if ok, retry := s.lim.Allow(key, limit, time.Hour); !ok {
		<-s.slots
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	signer, _, err := s.signerFor(ctx, spaceID)
	if err != nil {
		<-s.slots
		return err
	}
	var rc store.RegCode
	var code string
	if !probe {
		if rc, code, err = s.mach.IssueCode(ctx, spaceID, p.User.ID, t.profile); err != nil {
			<-s.slots
			return err
		}
	}
	kind := "machine_ssh"
	if probe {
		kind = "machine_ssh_probe"
	}
	s.auth.Audit(ctx, p.User.ID, kind, r, spaceID+" "+t.profile+" "+t.host)

	j := newJob(store.RandomHex(16))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.slots }()
		(&run{s: s, j: j, t: t, spaceID: spaceID, userID: p.User.ID, signer: signer, code: code, codeHash: rc.CodeHash}).do()
	}()
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": j.ID})
	return nil
}

// ---- the job as the page sees it ----

type step struct {
	ID     string  `json:"id"`
	State  string  `json:"state"`
	Detail *string `json:"detail"`
}

type jobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Provider *string   `json:"provider"`
	State    string    `json:"state"`
	Steps    []step    `json:"steps"`
	Error    *jobError `json:"error"`
	Check    any       `json:"check"`
}

func newJob(id string) *job {
	j := &job{ID: id, Kind: jobKind, State: "running"}
	for _, id := range []string{"ssh", "preflight", "install", "online"} {
		j.Steps = append(j.Steps, step{ID: id, State: "wait"})
	}
	return j
}

func (j *job) step(id string) *step {
	for i := range j.Steps {
		if j.Steps[i].ID == id {
			return &j.Steps[i]
		}
	}
	return nil
}

// ---- one run ----

type run struct {
	s       *Service
	j       *job
	t       target
	spaceID string
	userID  string
	// signer is the key pair of the space. code is the registration code of the install; an install that has not had
	// its host key confirmed has none.
	signer   ssh.Signer
	code     string
	codeHash string

	mu        sync.Mutex
	machineID string
}

// publish tells the space's owners the job as it is now. Machine id is empty until the machine registers.
func (r *run) publish() {
	r.mu.Lock()
	snapshot := *r.j
	snapshot.Steps = append([]step(nil), r.j.Steps...)
	id := r.machineID
	r.mu.Unlock()
	r.s.events.PublishJSON(r.spaceID, "job", map[string]any{"machineId": id, "job": snapshot}, true)
}

// set changes a step and publishes.
func (r *run) set(id, state string, detail string) {
	r.mu.Lock()
	st := r.j.step(id)
	st.State = state
	if detail != "" {
		d := clip(redact(detail, r.code), maxDetail)
		st.Detail = &d
	}
	r.mu.Unlock()
	r.publish()
}

// fail ends the job on the step that failed.
func (r *run) fail(stepID, code, message, detail string) {
	r.mu.Lock()
	r.j.State = "failed"
	r.j.Error = &jobError{Code: code, Message: message}
	r.mu.Unlock()
	r.set(stepID, "fail", detail)
}

func (r *run) done() {
	r.mu.Lock()
	r.j.State = "done"
	r.mu.Unlock()
	r.publish()
}

func redact(text, code string) string {
	if code == "" {
		return text
	}
	return strings.ReplaceAll(text, code, "hx_…")
}

func clip(v string, n int) string {
	r := []rune(v)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return v
}

func (r *run) do() {
	s := r.s
	ctx, cancel := context.WithTimeout(s.ctx, handshakeLimit+preflightLimit+installLimit+s.online+30*time.Second)
	defer cancel()
	r.publish()

	client, ok := r.connect(ctx)
	if !ok {
		return
	}
	// The session is forgotten as soon as the install ends: the hub keeps nothing to get back into the server with.
	defer client.Close()

	f, ok := r.look(ctx, client)
	if !ok {
		return
	}
	if !r.install(ctx, client, f) {
		return
	}
	_ = client.Close()
	r.waitOnline(ctx)
}

// connect is the ssh step: dial, handshake, key. The host key's fingerprint is shown, and it has to be the one the owner
// confirmed: a key nobody confirmed ends the job here, before the hub offers anything of its own, and so does a key that
// is another one.
func (r *run) connect(ctx context.Context) (*ssh.Client, bool) {
	r.set("ssh", "run", "")
	t := r.t
	cctx, cancel := context.WithTimeout(ctx, handshakeLimit)
	defer cancel()
	conn, err := r.s.dial(cctx, "tcp", t.addr())
	if err != nil {
		code, msg := r.s.dialProblem(err, t)
		r.fail("ssh", code, msg, "")
		return nil, false
	}
	var seen, seenType string
	var unconfirmed, mismatch bool
	cfg := &ssh.ClientConfig{
		User: t.user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(r.signer)},
		// The kinds of host key a person can compare: ed25519 first, which is what most servers print.
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen, seenType = ssh.FingerprintSHA256(key), key.Type()
			r.set("ssh", "run", seen)
			switch {
			case t.fingerprint == "":
				unconfirmed = true
				return errHostKeyUnconfirmed
			case subtle.ConstantTimeCompare([]byte(seen), []byte(t.fingerprint)) != 1:
				mismatch = true
				return errHostKeyMismatch
			}
			return nil
		},
		Timeout: handshakeLimit,
	}
	// The handshake has its own deadline: ssh.NewClientConn does not look at the context.
	_ = conn.SetDeadline(time.Now().Add(handshakeLimit))
	cc, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		conn.Close()
		switch {
		case unconfirmed:
			r.fail("ssh", "host_key_unconfirmed", fmt.Sprintf("Ключ сервера %s: %s. Сверьте отпечаток с тем, что показывает сам сервер (ssh-keygen -lf /etc/ssh/ssh_host_%s_key.pub), и подтвердите подключение: хаб ничего не выполнял и ключ не предъявлял", seenType, seen, hostKeyKind(seenType)), seen)
		case mismatch:
			r.fail("ssh", "host_key_mismatch", fmt.Sprintf("Ключ сервера не совпал с подтверждённым отпечатком (сервер показал %s). Это другой сервер или подмена: проверьте адрес. Хаб ничего не выполнял и ключ не предъявлял", seen), seen)
		default:
			code, msg := handshakeProblem(err, t)
			r.fail("ssh", code, msg, seen)
		}
		return nil, false
	}
	_ = conn.SetDeadline(time.Time{})
	r.set("ssh", "ok", "")
	return ssh.NewClient(cc, chans, reqs), true
}

// hostKeyKind is how sshd names the file of a kind of host key: ed25519, ecdsa, rsa.
func hostKeyKind(keyType string) string {
	switch {
	case strings.Contains(keyType, "ed25519"):
		return "ed25519"
	case strings.Contains(keyType, "ecdsa"):
		return "ecdsa"
	default:
		return "rsa"
	}
}

var (
	errHostKeyUnconfirmed = errors.New("host key not confirmed")
	errHostKeyMismatch    = errors.New("host key is not the confirmed one")
)

// dialProblem tells the owner why the server could not be reached. A refused port, a name that does not resolve, a
// silent address all read the same: telling them apart would make the hub a scanner of whatever its network sees.
func (s *Service) dialProblem(err error, t target) (string, string) {
	if errors.Is(err, errBlocked) {
		if s.cfg.Edition == config.EditionCloud {
			return "ssh_blocked", "Этот адрес недоступен для подключения по SSH: хаб подключается только к серверам в публичном интернете"
		}
		return "ssh_blocked", "Этот адрес недоступен для подключения по SSH: хаб подключается только к серверам в публичном интернете. " +
			"Для сервера во внутренней сети администратор хаба включает HUB_SSH_ALLOW_PRIVATE"
	}
	return "ssh_unreachable", fmt.Sprintf("Не достучались до %s: проверьте адрес и порт, что там запущен SSH и что сервер виден из сети хаба", t.addr())
}

func handshakeProblem(err error, t target) (string, string) {
	if strings.Contains(err.Error(), "unable to authenticate") {
		return "ssh_auth", fmt.Sprintf("Сервер не принял ключ вашего пространства. Добавьте его в ~/.ssh/authorized_keys пользователя %s и попробуйте снова", t.user)
	}
	return "ssh_unreachable", fmt.Sprintf("Не получилось договориться по SSH с %s", t.addr())
}

// look is the preflight step: what the server is, whether it has room, and whether the profile can be installed.
func (r *run) look(ctx context.Context, client *ssh.Client) (facts, bool) {
	r.set("preflight", "run", "")
	space := "/var/lib"
	if r.t.profile == "full" {
		space = `"$HOME"`
	}
	out, _, err := execute(ctx, client, inSh(preflightScript(space)), preflightLimit)
	if err != nil {
		r.fail("preflight", "preflight", "Не получилось посмотреть сервер: "+short(err), "")
		return facts{}, false
	}
	f := parseFacts(out)
	if msg := f.problem(r.t.profile); msg != "" {
		r.fail("preflight", "preflight", msg, f.summary())
		return f, false
	}
	r.set("preflight", "ok", f.summary())
	return f, true
}

// install runs the installer and reads the end of its output when it fails.
func (r *run) install(ctx context.Context, client *ssh.Client, f facts) bool {
	r.set("install", "run", "")
	script := installScript(r.s.cfg.PublicURL, r.code, r.t.profile, f.UID)
	out, status, err := execute(ctx, client, inSh(script), installLimit)
	switch {
	case err != nil && status < 0:
		r.fail("install", "install_failed", "Установка прервалась: "+short(err), tail(redact(out, r.code)))
		return false
	case status != 0:
		r.fail("install", "install_failed", fmt.Sprintf("Установщик завершился с ошибкой (код %d)", status), tail(redact(out, r.code)))
		return false
	}
	r.set("install", "ok", "")
	return true
}

// waitOnline is the last step: the machine registered with this job's code and has dialed in.
func (r *run) waitOnline(ctx context.Context) {
	r.set("online", "run", "")
	s := r.s
	deadline := time.NewTimer(s.online)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		rc, err := s.db.S().RegCodeByHash(ctx, r.codeHash)
		if err == nil && rc.MachineID != "" {
			if r.machineID == "" {
				r.mu.Lock()
				r.machineID = rc.MachineID
				r.mu.Unlock()
				// The installer does not say how it was started: the hub knows.
				if err := s.db.S().SetMachineVia(ctx, rc.MachineID, string(hubproto.ViaSSH)); err != nil {
					s.log.Warn("machine via not saved", "machine", rc.MachineID, "error", err)
				}
			}
			if s.reg.Get(rc.MachineID) != nil {
				r.set("online", "ok", "")
				r.done()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			msg := "Агент поставился, но не вышел на связь с хабом. Проверьте, что сервер открывает " + s.cfg.PublicURL
			if r.machineID == "" {
				msg = "Установщик отработал, но машина не зарегистрировалась в хабе. Проверьте, что сервер открывает " + s.cfg.PublicURL
			}
			r.fail("online", "not_online", msg, "")
			return
		case <-tick.C:
		}
	}
}

// execute runs a command and returns its output (bounded) and exit status; status is -1 when it did not run to an end.
func execute(ctx context.Context, client *ssh.Client, cmd string, limit time.Duration) (string, int, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer sess.Close()
	out := &limited{max: maxOutput}
	sess.Stdout, sess.Stderr = out, out

	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err := <-done:
		var exit *ssh.ExitError
		switch {
		case err == nil:
			return out.String(), 0, nil
		case errors.As(err, &exit):
			return out.String(), exit.ExitStatus(), err
		default:
			return out.String(), -1, err
		}
	case <-ctx.Done():
		_ = sess.Close()
		return out.String(), -1, errors.New("не дождались ответа сервера")
	}
}

// limited keeps the first max bytes written to it and drops the rest, safe for the two streams of a session.
type limited struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (l *limited) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (l *limited) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// tail is the last lines of an output, for the page.
func tail(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return strings.Join(lines, "\n")
}

func short(err error) string { return clip(err.Error(), 200) }

// ---- the network ----

var errBlocked = errors.New("address is not on the public internet")

// lookupNetIP resolves a name; tests replace it.
var lookupNetIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// networkDialer connects over TCP. Unless private addresses are allowed (HUB_SSH_ALLOW_PRIVATE, a self-hosted hub with
// servers on its LAN) it refuses every address that is not on the public internet: the person typing an address must
// not reach the network the hub sits in, its metadata service, its databases or its own loopback. The name is resolved
// once, every address it resolves to has to pass, and the connection is made to one of those addresses, not to the
// name: a name that answers one address to the check and another to the connection (DNS rebinding) gets no second
// chance. The dialer's own Control checks the address once more where the connection is really made.
func networkDialer(allowPrivate bool) Dialer {
	d := &net.Dialer{Timeout: dialTimeout}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errBlocked
			}
			if a, err := netip.ParseAddr(host); err != nil || !publicAddr(a) {
				return errBlocked
			}
			return nil
		}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		if !allowPrivate {
			for _, ip := range ips {
				if !publicAddr(ip) {
					return nil, errBlocked
				}
			}
		}
		var last error
		for _, ip := range ips {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
			if errors.Is(err, errBlocked) {
				return nil, err
			}
			last = err
		}
		return nil, last
	}
}

// resolve is a literal address as it is, a name through the resolver.
func resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")); err == nil {
		return []netip.Addr{a}, nil
	}
	ips, err := lookupNetIP(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	return ips, nil
}

// notPublic4 and notPublic6 are the special-purpose ranges of the IANA registries (RFC 6890 and what followed) that the
// open internet does not route, besides what netip.Addr says of loopback, link-local, multicast and the unspecified
// address.
var notPublic4 = prefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
)

var notPublic6 = prefixes(
	"2001::/23",     // IETF protocol assignments (Teredo, 2001::/32, is looked into before this)
	"2001:db8::/32", // documentation
	"3fff::/20",     // documentation
)

func prefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, p := range list {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

var (
	global6   = netip.MustParsePrefix("2000::/3")
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
	teredo    = netip.MustParsePrefix("2001::/32")
)

// publicAddr says an address is one the open internet routes. IPv4 addresses are checked against the special-purpose
// list; IPv6 ones must be global unicast (2000::/3) and out of that list. An address that carries an IPv4 one inside
// (IPv4-mapped ::ffff:a.b.c.d, NAT64 64:ff9b::a.b.c.d, 6to4 2002:AABB:CCDD::, Teredo, which carries a server's and a
// client's) is only as public as the IPv4 address in it: they are how a loopback or a private address is spelled to a
// check that only knows the plain forms.
func publicAddr(a netip.Addr) bool {
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	if a.Is4In6() {
		return publicAddr(a.Unmap())
	}
	if a.Is4() {
		return public4(a)
	}
	b := a.As16()
	switch {
	case nat64.Contains(a):
		return public4(netip.AddrFrom4([4]byte(b[12:16])))
	case sixToFour.Contains(a):
		return public4(netip.AddrFrom4([4]byte(b[2:6])))
	case teredo.Contains(a):
		client := [4]byte{b[12] ^ 0xff, b[13] ^ 0xff, b[14] ^ 0xff, b[15] ^ 0xff}
		return public4(netip.AddrFrom4([4]byte(b[4:8]))) && public4(netip.AddrFrom4(client))
	}
	if !global6.Contains(a) || !a.IsGlobalUnicast() {
		return false
	}
	for _, p := range notPublic6 {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

func public4(a netip.Addr) bool {
	if !a.IsGlobalUnicast() {
		return false
	}
	for _, p := range notPublic4 {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
