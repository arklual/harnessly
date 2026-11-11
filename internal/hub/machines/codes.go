package machines

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

const (
	// CodeTTL is how long a registration code can be used.
	CodeTTL = 30 * time.Minute
	// maxActiveCodes bounds the unused codes of a space.
	maxActiveCodes = 20
	// registrationsPerHour is the limit on registration attempts per address.
	registrationsPerHour = 30
	// localRenewBelow is when the local code is replaced by a fresher one.
	localRenewBelow = 10 * time.Minute
	localEvery      = time.Minute
)

var errBadCode = httpx.New(http.StatusGone, "expired", "Код не подошёл или устарел: возьмите новый в harnessly")

// newCode is a registration code: hx_ and 24 base32 characters (120 bits).
func newCode() string { return "hx_" + store.RandomBase32(15) }

// InstallCommand is the line a person pastes on the machine. The dir profile makes a user and system services, so its
// command runs the script with sudo; full and container run as the person who pastes it.
func (s *Service) InstallCommand(code, profile string) string {
	shell := "sh"
	if hubproto.Profile(profile) == hubproto.ProfileDir {
		shell = "sudo sh"
	}
	return "curl -fsSL " + s.cfg.AgentURL + "/install | " + shell + " -s -- --token " + code + " --profile " + profile
}

func (s *Service) createCode(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	var req struct {
		Profile     string
		ConfirmFull bool
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	spaceID := t.SpaceID
	if err := s.CheckProfile(req.Profile, req.ConfirmFull); err != nil {
		return err
	}
	rc, code, err := s.IssueCode(ctx, spaceID, p.User.ID, req.Profile)
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_code", r, spaceID+" "+req.Profile)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"code": code, "expiresAt": httpx.Time(rc.ExpiresAt), "profile": req.Profile, "command": s.InstallCommand(code, req.Profile),
	})
	return nil
}

// IssueCode makes a registration code for a space, under the limits on codes per person and per space. The caller has
// checked that the person owns the space and that the profile is allowed (CheckProfile). It returns the stored row
// and the code, which is not kept anywhere else.
func (s *Service) IssueCode(ctx context.Context, spaceID, userID, profile string) (store.RegCode, string, error) {
	if ok, retry := s.lim.Allow("code:"+userID, 30, time.Hour); !ok {
		return store.RegCode{}, "", httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	now := s.now().UTC()
	st := s.db.S()
	if n, err := st.CountActiveRegCodes(ctx, spaceID, now); err != nil {
		return store.RegCode{}, "", err
	} else if n >= maxActiveCodes {
		return store.RegCode{}, "", httpx.Conflict("code_limit", "Слишком много неиспользованных кодов: подождите, пока они истекут")
	}
	code := newCode()
	rc := store.RegCode{CodeHash: store.HashToken(code), SpaceID: spaceID, CreatedBy: userID, Profile: profile, ExpiresAt: now.Add(CodeTTL)}
	if err := st.CreateRegCode(ctx, rc); err != nil {
		return store.RegCode{}, "", err
	}
	return rc, code, nil
}

// CheckProfile validates an isolation profile and what choosing it needs: the full profile is confirmed, the
// container profile needs an image the hub knows.
func (s *Service) CheckProfile(profile string, confirmFull bool) error {
	switch hubproto.Profile(profile) {
	case hubproto.ProfileDir:
	case hubproto.ProfileFull:
		if !confirmFull {
			return httpx.Invalid("confirmFull", "Подтвердите: агенты получат всё, что доступно вашей учётной записи")
		}
	case hubproto.ProfileContainer:
		if s.cfg.ContainerImage == "" {
			return httpx.ErrProviderOff
		}
	default:
		return httpx.Invalid("profile", "Выберите режим изоляции: dir, container или full")
	}
	return nil
}

// DialURL is where agents dial: the public URL with ws(s) and /agent/v1.
func DialURL(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + strings.TrimRight(u.Path, "/") + "/agent/v1"
}

func printable(v string, max int) (string, bool) {
	if len([]rune(v)) > max {
		return "", false
	}
	for _, c := range v {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	return strings.TrimSpace(v), true
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) error {
	ip := ratelimit.IPKey(s.auth.ClientIP(r))
	if ok, retry := s.lim.Allow("register:"+ip, registrationsPerHour, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	var req hubproto.RegisterRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	signKey, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil || len(signKey) != ed25519.PublicKeySize {
		return httpx.Invalid("publicKey", "Ключ подписи — 32 байта в base64")
	}
	encKey, err := base64.StdEncoding.DecodeString(req.EncKey)
	if err != nil {
		return httpx.Invalid("encKey", "Ключ шифрования — 32 байта в base64")
	}
	if _, err := ecdh.X25519().NewPublicKey(encKey); err != nil {
		return httpx.Invalid("encKey", "Ключ шифрования — 32 байта в base64")
	}
	hostname, ok1 := printable(req.Hostname, 255)
	unixUser, ok2 := printable(req.UnixUser, 64)
	version, ok3 := printable(req.AgentVersion, 64)
	switch {
	case !ok1:
		return httpx.Invalid("hostname", "Некорректное имя машины")
	case !ok2:
		return httpx.Invalid("unixUser", "Некорректное имя пользователя")
	case !ok3:
		return httpx.Invalid("agentVersion", "Некорректная версия")
	case !oneOf(req.OS, "linux", "darwin"):
		return httpx.Invalid("os", "Поддерживаются linux и darwin")
	case !oneOf(req.Arch, "amd64", "arm64"):
		return httpx.Invalid("arch", "Поддерживаются amd64 и arm64")
	case !req.Via.Valid():
		return httpx.Invalid("via", "Неизвестный способ подключения")
	}
	if hostname == "" {
		hostname = "machine"
	}

	ctx := r.Context()
	hash := store.HashToken(strings.TrimSpace(req.Code))
	code, err := s.db.S().RegCodeByHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return errBadCode
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if code.UsedAt != nil || !code.ExpiresAt.After(now) {
		return errBadCode
	}

	m := store.Machine{
		ID: store.NewID(), SpaceID: code.SpaceID, Name: hostname, Hostname: hostname, OS: req.OS, Arch: req.Arch, UnixUser: unixUser,
		// The code's profile wins: it is what the owner chose, and the install command carried it.
		Profile: code.Profile, Via: string(req.Via), KeyPub: base64.StdEncoding.EncodeToString(signKey), EncPub: base64.StdEncoding.EncodeToString(encKey),
		AgentVersion: version, CreatedBy: code.CreatedBy, CreatedAt: now,
	}
	if code.Local {
		m.Via = string(hubproto.ViaLocal)
	} else if m.Via == string(hubproto.ViaLocal) {
		m.Via = string(hubproto.ViaCommand)
	}
	// A machine that proves it holds the key of its registration in this space keeps that registration's id, name and
	// place; only what it says of itself and its keys are new.
	prev, again := s.heldBefore(ctx, req, code)
	if again {
		prev.Hostname, prev.OS, prev.Arch, prev.UnixUser, prev.AgentVersion = m.Hostname, m.OS, m.Arch, m.UnixUser, m.AgentVersion
		prev.Profile, prev.Via, prev.KeyPub, prev.EncPub = m.Profile, m.Via, m.KeyPub, m.EncPub
		m = prev
	}
	var replaced []string
	err = s.db.Tx(ctx, func(st store.Store) error {
		if ok, err := st.ConsumeRegCode(ctx, hash, m.ID, now); err != nil {
			return err
		} else if !ok {
			return errBadCode
		}
		if again {
			if ok, err := st.RekeyMachine(ctx, m); err != nil {
				return err
			} else if !ok {
				// Removed or switched off between the check and here: the code is not spent, the next try registers anew.
				return httpx.Conflict("machine_gone", "Машину убрали или выключили в harnessly: повторите регистрацию")
			}
			return nil
		}
		if code.Local {
			var err error
			if replaced, err = st.RevokeUnclaimedLocal(ctx, now); err != nil {
				return err
			}
		}
		return st.CreateMachine(ctx, &m)
	})
	if store.IsUnique(err) {
		return httpx.Conflict("key_taken", "Эта машина уже зарегистрирована")
	}
	if err != nil {
		return err
	}
	for _, id := range replaced {
		s.reg.Revoke(id)
		if s.revoked != nil {
			s.revoked(id)
		}
	}
	action, status := "machine_registered", http.StatusCreated
	if again {
		// The link the machine may still hold was opened with the key that no longer is its own.
		s.reg.Revoke(m.ID)
		action, status = "machine_reregistered", http.StatusOK
	}
	s.auth.Audit(ctx, code.CreatedBy, action, r, m.ID+" "+m.Profile+" "+m.Via)

	res := hubproto.RegisterResponse{MachineID: m.ID, DialURL: DialURL(s.cfg.AgentURL), RuntimeBasePath: "/m/" + m.ID, HubURL: s.cfg.AgentURL}
	if m.SpaceID != "" {
		sp, err := s.db.S().SpaceByID(ctx, m.SpaceID)
		if err != nil {
			return err
		}
		res.SpaceName = &sp.Name
		s.Publish(ctx, m)
	}
	if code.Local {
		safego.Go("machines register", func() {
			bg, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := s.renewLocal(bg); err != nil {
				s.log.Warn("local registration code not renewed", "error", err)
			}
		})
	}
	httpx.WriteJSON(w, status, res)
	return nil
}

// heldBefore is the machine a registration claims to be (RegisterRequest.Previous), when the claim holds: the machine
// is in the space the code is for, is neither removed nor switched off, and the claim is signed with the key the hub
// holds for it. Anything else, a wrong signature included, is not an error: the registration makes a new machine, as
// it does without a claim, so knowing a machine's id never gets anybody that machine.
func (s *Service) heldBefore(ctx context.Context, req hubproto.RegisterRequest, code store.RegCode) (store.Machine, bool) {
	c := req.Previous
	if c == nil || code.Local || code.SpaceID == "" || c.MachineID == "" || len(c.MachineID) > 64 {
		return store.Machine{}, false
	}
	m, err := s.db.S().MachineByID(ctx, c.MachineID)
	if err != nil || m.SpaceID != code.SpaceID || m.RevokedAt != nil || m.DisabledAt != nil {
		return store.Machine{}, false
	}
	held, err := base64.StdEncoding.DecodeString(m.KeyPub)
	if err != nil || !hubproto.VerifyReregister(ed25519.PublicKey(held), c, strings.TrimSpace(req.Code), req.PublicKey) {
		s.log.Warn("a registration claimed a machine's id without its key", "machine", m.ID)
		return store.Machine{}, false
	}
	return m, true
}

func (s *Service) claimLocal(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	ctx := r.Context()
	spaceID := t.SpaceID
	if !p.Admin {
		return httpx.New(http.StatusForbidden, "forbidden", "Подключить эту машину может только администратор установки")
	}
	if s.cfg.Edition == config.EditionCloud {
		return httpx.ErrNotFound
	}
	st := s.db.S()
	m, err := st.UnclaimedLocalMachine(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if ok, err := st.ClaimMachine(ctx, m.ID, spaceID, p.User.ID); err != nil {
		return err
	} else if !ok {
		return httpx.ErrNotFound
	}
	m.SpaceID, m.CreatedBy = spaceID, p.User.ID
	s.auth.Audit(ctx, p.User.ID, "machine_claimed", r, m.ID+" "+spaceID)
	v, err := s.View(ctx, m)
	if err != nil {
		return err
	}
	s.Publish(ctx, m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": v})
	return nil
}

// LocalAvailable says whether an unclaimed local machine is registered: «Эта машина» in the UI.
func (s *Service) LocalAvailable(ctx context.Context) bool {
	if s.cfg.Edition == config.EditionCloud {
		return false
	}
	_, err := s.db.S().UnclaimedLocalMachine(ctx)
	return err == nil
}

// RunLocalCode keeps a registration code for the machine next to the hub in HUB_LOCAL_CODE_FILE (compose.hub.yaml
// shares the file with the machine's container): a fresh one at start, and a new one whenever it is used, near its
// end, or the file no longer holds a code the hub knows. It runs until ctx ends. Without the setting it returns at once.
func (s *Service) RunLocalCode(ctx context.Context) {
	if s.cfg.LocalCodeFile == "" {
		return
	}
	tick := time.NewTicker(localEvery)
	defer tick.Stop()
	for {
		if err := s.EnsureLocalCode(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("local registration code not ready", "file", s.cfg.LocalCodeFile, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// EnsureLocalCode renews the local code when it needs it.
func (s *Service) EnsureLocalCode(ctx context.Context) error {
	if s.cfg.LocalCodeFile == "" {
		return nil
	}
	s.localMu.Lock()
	defer s.localMu.Unlock()
	cur, err := s.db.S().ActiveLocalRegCode(ctx, s.now())
	if err == nil && cur.ExpiresAt.Sub(s.now()) > localRenewBelow {
		if b, err := os.ReadFile(s.cfg.LocalCodeFile); err == nil && store.HashToken(strings.TrimSpace(string(b))) == cur.CodeHash {
			return nil
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return s.writeLocalCode(ctx)
}

func (s *Service) renewLocal(ctx context.Context) error {
	if s.cfg.LocalCodeFile == "" {
		return nil
	}
	s.localMu.Lock()
	defer s.localMu.Unlock()
	return s.writeLocalCode(ctx)
}

// writeLocalCode makes a new local code and puts it in the file. The caller holds localMu.
func (s *Service) writeLocalCode(ctx context.Context) error {
	st := s.db.S()
	code := newCode()
	if err := st.DeleteLocalRegCodes(ctx); err != nil {
		return err
	}
	rc := store.RegCode{CodeHash: store.HashToken(code), Profile: string(hubproto.ProfileContainer), Local: true, ExpiresAt: s.now().UTC().Add(CodeTTL)}
	if err := st.CreateRegCode(ctx, rc); err != nil {
		return err
	}
	return writeSecretFileAtomic(s.cfg.LocalCodeFile, []byte(code+"\n"), s.cfg.LocalCodeGID)
}

// writeSecretFileAtomic replaces a file that holds a secret in one step, so a reader sees the old code or the new one
// and never half of it. The file is for the hub alone (0600, a folder made for it 0700); with gid >= 0 it is also
// readable by that group (0640, and a folder made for it 0750), which is how a machine's container reads its code from
// the volume it shares with the hub: the hub must be a member of the group, or the write fails rather than leaving the
// code wider open than asked.
func writeSecretFileAtomic(path string, data []byte, gid int) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		dirMode := os.FileMode(0o700)
		if gid >= 0 {
			dirMode = 0o750
		}
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return err
		}
		if gid >= 0 {
			if err := os.Chown(dir, -1, gid); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".code-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	mode := os.FileMode(0o600)
	if gid >= 0 {
		mode = 0o640
		if err := tmp.Chown(-1, gid); err != nil {
			tmp.Close()
			os.Remove(name)
			return err
		}
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
