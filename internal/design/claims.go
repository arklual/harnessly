package design

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/text/unicode/norm"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Board claims: a soft lock the chats of the library's runtimes keep in the shared folder, telling that a board is
// being edited by a chat. A claim is <library>/.claims/<project>/<board>-<hash>.json, refreshed by the owning runtime
// while the chat works and released when its turn ends; one nobody refreshes for three minutes is free. The folder is
// as untrusted as the boards: every level is opened without following a link and every entry is used relative to
// those descriptors, a file that is not a small regular file is ignored, every text read back is cleaned.

const ClaimsDirname = ".claims"

const (
	claimHeartbeatMs = 30_000
	claimStaleMs     = 3 * 60_000
	claimGraceMs     = 20_000
	claimGoneMs      = 15_000
	claimSkewMs      = 60_000
	maxClaimBytes    = 4096
	maxClaimEntries  = 500
)

var (
	claimTabIDRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	claimRuntimeRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	claimSlugRe    = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	edgeDashes     = regexp.MustCompile(`^-+|-+$`)
)

// ClaimError is a refusal worded for the person or the agent: the API returns it as is.
type ClaimError struct{ Msg string }

func (e *ClaimError) Error() string { return e.Msg }

func claimErr(msg string) error { return &ClaimError{Msg: msg} }

type ClaimOwner struct {
	User, Runtime, TabID, ChatTitle string
}

type Claim struct {
	Project, Board   string
	Owner            ClaimOwner
	Since, Heartbeat int64
}

// CleanBoardKey is a board key an agent or a person typed: one line, and no way to be a path.
func CleanBoardKey(v any) (string, error) {
	b, ok := CleanText(v, 120)
	if !ok || b == "." || b == ".." || strings.ContainsAny(b, `\/`) {
		return "", claimErr(locale.Pick("Name the board by its name, without a path", "Назовите доску её именем, без пути"))
	}
	return b, nil
}

func timestamp(v any) (int64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, false
	}
	return round(f), true
}

// ParseClaim reads a claim file's document; ok false when it is not a claim.
func ParseClaim(raw any) (Claim, bool) {
	obj, ok := raw.(*jsvalue.Object)
	if !ok {
		return Claim{}, false
	}
	owner := obj.Object("owner")
	if owner == nil {
		return Claim{}, false
	}
	project, _ := get(obj, "project").(string)
	if !IsLibraryID(project) {
		return Claim{}, false
	}
	board, okBoard := CleanText(get(obj, "board"), 120)
	since, okSince := timestamp(get(obj, "since"))
	hb, okHB := timestamp(get(obj, "heartbeatAt"))
	if !okBoard || strings.ContainsAny(board, `\/`) || !okSince || !okHB {
		return Claim{}, false
	}
	person, okPerson := CleanText(get(owner, "user"), 64)
	runtime, okRuntime := get(owner, "runtime").(string)
	tabID, okTab := get(owner, "tabId").(string)
	if !okPerson || !okRuntime || !claimRuntimeRe.MatchString(runtime) || !okTab || !claimTabIDRe.MatchString(tabID) {
		return Claim{}, false
	}
	title, okTitle := CleanText(get(owner, "chatTitle"), 120)
	if !okTitle {
		title = locale.Pick("Agent", "Агент")
	}
	return Claim{Project: project, Board: board, Owner: ClaimOwner{User: person, Runtime: runtime, TabID: tabID, ChatTitle: title}, Since: since, Heartbeat: hb}, true
}

func (c Claim) document() []byte {
	o := jsvalue.NewObject("project", c.Project, "board", c.Board,
		"owner", jsvalue.NewObject("user", c.Owner.User, "runtime", c.Owner.Runtime, "tabId", c.Owner.TabID, "chatTitle", c.Owner.ChatTitle),
		"since", float64(c.Since), "heartbeatAt", float64(c.Heartbeat))
	return append(jsvalue.Stringify(o, "  "), '\n')
}

// ClaimFileName is readable, cut to a safe length, and told apart from similar names by a hash of the exact key.
func ClaimFileName(board string) string {
	slug := jscompat.Slice(edgeDashes.ReplaceAllString(claimSlugRe.ReplaceAllString(norm.NFKC.String(board), "-"), ""), 60)
	if slug == "" {
		slug = "board"
	}
	sum := sha1.Sum([]byte(board))
	return slug + "-" + hex.EncodeToString(sum[:])[:8] + ".json"
}

// IsClaimLive: fresh by its heartbeat; a heartbeat from the future is no heartbeat.
func IsClaimLive(hb, now int64) bool {
	age := now - hb
	return age < claimStaleMs && age > -claimSkewMs
}

func sameOwner(a, b ClaimOwner) bool { return a.Runtime == b.Runtime && a.TabID == b.TabID }

func viaFd(f *os.File, name string) string {
	if name == "" {
		return fmt.Sprintf("/proc/self/fd/%d", f.Fd())
	}
	return fmt.Sprintf("/proc/self/fd/%d/%s", f.Fd(), name)
}

func openDirNoFollow(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
}

// openChild is one level down from an open folder, never through a link; nil when it is not there and create is off.
func openChild(parent *os.File, name string, create bool) (*os.File, error) {
	target := viaFd(parent, name)
	f, err := openDirNoFollow(target)
	if err == nil {
		return f, nil
	}
	if errors.Is(err, syscall.ENOENT) {
		if !create {
			return nil, nil
		}
		if err := os.Mkdir(target, 0o770); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		return openDirNoFollow(target)
	}
	if create && (errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR)) {
		return nil, claimErr(ClaimsDirname + "/" + name + locale.Pick(" in the library is not a folder: claims are not accepted until it is removed", " в библиотеке не папка: заявки не принимаются, пока её не уберут"))
	}
	return nil, nil
}

func (l *Library) openProjectClaims(project string, create bool) (*os.File, error) {
	if !IsLibraryID(project) {
		return nil, nil
	}
	root, err := os.OpenFile(l.Root(), os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, nil
	}
	defer root.Close()
	claims, err := openChild(root, ClaimsDirname, create)
	if claims == nil || err != nil {
		return nil, err
	}
	defer claims.Close()
	return openChild(claims, project, create)
}

func readClaimFile(dir *os.File, name string) (Claim, bool) {
	f, err := os.OpenFile(viaFd(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Claim{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxClaimBytes {
		return Claim{}, false
	}
	buf := make([]byte, st.Size())
	n, _ := f.ReadAt(buf, 0)
	raw, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(buf[:n])))
	if err != nil {
		return Claim{}, false
	}
	c, ok := ParseClaim(raw)
	if !ok || ClaimFileName(c.Board) != name {
		return Claim{}, false
	}
	return c, true
}

func claimNames(dir *os.File) []string {
	names, _ := readNames(viaFd(dir, ""))
	var out []string
	for _, n := range names {
		if strings.HasSuffix(n, ".json") && !strings.HasPrefix(n, ".") {
			out = append(out, n)
		}
	}
	if len(out) > maxClaimEntries {
		out = out[:maxClaimEntries]
	}
	return out
}

// readNames lists a folder sorted by name, as node's readdir does.
func readNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeTemp(dir *os.File, c Claim) (string, error) {
	name := "." + randomHex(6) + ".tmp"
	f, err := os.OpenFile(viaFd(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o660)
	if err != nil {
		return "", err
	}
	_, err = f.Write(c.document())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return name, err
}

func dropTemp(dir *os.File, name string) { _ = os.Remove(viaFd(dir, name)) }

func overwrite(dir *os.File, c Claim) error {
	temp, err := writeTemp(dir, c)
	if err != nil {
		if temp != "" {
			dropTemp(dir, temp)
		}
		return err
	}
	if err := os.Rename(viaFd(dir, temp), viaFd(dir, ClaimFileName(c.Board))); err != nil {
		dropTemp(dir, temp)
		return err
	}
	return nil
}

// createFresh makes the claim only if nobody has: link fails where rename would replace.
func createFresh(dir *os.File, c Claim) (bool, error) {
	temp, err := writeTemp(dir, c)
	if temp != "" {
		defer dropTemp(dir, temp)
	}
	if err != nil {
		return false, err
	}
	if err := os.Link(viaFd(dir, temp), viaFd(dir, ClaimFileName(c.Board))); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func removeFile(dir *os.File, name string) bool {
	err := os.Remove(viaFd(dir, name))
	return err == nil || errors.Is(err, os.ErrNotExist)
}

// serialized: the chats of one runtime reach a project's claims from one process and queue instead of racing.
func (l *Library) serialized(project string, task func() error) error {
	m := l.lockOf("claims\x00" + project)
	m.Lock()
	defer m.Unlock()
	return task()
}

// ClaimResult is a claim taken (Held nil) or refused with its holder.
type ClaimResult struct {
	Claim     Claim
	Refreshed bool
	Held      *Claim
}

// ClaimBoard takes a board for a chat: its own claim is refreshed (since stays), one held by another live chat is
// refused with the holder, a stale or unreadable file is cleared away first.
func (l *Library) ClaimBoard(project, board string, owner ClaimOwner, now int64) (ClaimResult, error) {
	var out ClaimResult
	err := l.serialized(project, func() error {
		dir, err := l.openProjectClaims(project, true)
		if err != nil {
			return err
		}
		if dir == nil {
			return claimErr(locale.Pick("No such project in the library", "Нет такого проекта в библиотеке"))
		}
		defer dir.Close()
		name := ClaimFileName(board)
		for attempt := 0; attempt < 3; attempt++ {
			existing, ok := readClaimFile(dir, name)
			if ok && IsClaimLive(existing.Heartbeat, now) {
				if !sameOwner(existing.Owner, owner) {
					out.Held = &existing
					return nil
				}
				refreshed := existing
				refreshed.Owner, refreshed.Heartbeat = owner, now
				if err := overwrite(dir, refreshed); err != nil {
					return err
				}
				out = ClaimResult{Claim: refreshed, Refreshed: true}
				return nil
			}
			_, lerr := os.Lstat(viaFd(dir, name))
			if ok || lerr == nil {
				if again, ok := readClaimFile(dir, name); ok && IsClaimLive(again.Heartbeat, now) {
					continue
				}
				if !removeFile(dir, name) {
					return claimErr(locale.Pick("Could not release the old claim on the board", "Не удалось освободить старую заявку на доску"))
				}
			}
			c := Claim{Project: project, Board: board, Owner: owner, Since: now, Heartbeat: now}
			created, err := createFresh(dir, c)
			if err != nil {
				return err
			}
			if created {
				out = ClaimResult{Claim: c}
				return nil
			}
		}
		return claimErr(locale.Pick("Another chat has just taken the board: try again in a few seconds", "Доску только что взял другой чат: повторите через несколько секунд"))
	})
	return out, err
}

type claimEntry struct {
	claim Claim
	name  string
}

func (l *Library) entriesOf(project string) []claimEntry {
	dir, _ := l.openProjectClaims(project, false)
	if dir == nil {
		return nil
	}
	defer dir.Close()
	var out []claimEntry
	for _, name := range claimNames(dir) {
		if c, ok := readClaimFile(dir, name); ok && c.Project == project {
			out = append(out, claimEntry{claim: c, name: name})
		}
	}
	return out
}

func (l *Library) projectsWithClaims() []string {
	root, err := os.OpenFile(l.Root(), os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil
	}
	defer root.Close()
	claims, _ := openChild(root, ClaimsDirname, false)
	if claims == nil {
		return nil
	}
	defer claims.Close()
	names, _ := readNames(viaFd(claims, ""))
	var out []string
	for _, n := range names {
		if IsLibraryID(n) {
			out = append(out, n)
		}
	}
	if len(out) > maxClaimEntries {
		out = out[:maxClaimEntries]
	}
	return out
}

// scanClaims is every readable claim, live or not, of one project ("" for the whole library).
func (l *Library) scanClaims(project string) []claimEntry {
	projects := []string{project}
	if project == "" {
		projects = l.projectsWithClaims()
	}
	var out []claimEntry
	for _, p := range projects {
		out = append(out, l.entriesOf(p)...)
	}
	return out
}

func sameClaim(a, b Claim) bool { return sameOwner(a.Owner, b.Owner) && a.Since == b.Since }

// dropClaim removes a claim file if it is still the one that was read: a refresh in between keeps it.
func (l *Library) dropClaim(e claimEntry, now int64, onlyIfStale bool) bool {
	dropped := false
	_ = l.serialized(e.claim.Project, func() error {
		dir, _ := l.openProjectClaims(e.claim.Project, false)
		if dir == nil {
			return nil
		}
		defer dir.Close()
		cur, ok := readClaimFile(dir, e.name)
		if !ok || !sameClaim(cur, e.claim) || (onlyIfStale && IsClaimLive(cur.Heartbeat, now)) {
			return nil
		}
		dropped = removeFile(dir, e.name)
		return nil
	})
	return dropped
}

// LiveClaims are the live claims of a project ("" for all); the stale ones seen are removed.
func (l *Library) LiveClaims(project string, now int64) []Claim {
	var live []Claim
	for _, e := range l.scanClaims(project) {
		if IsClaimLive(e.claim.Heartbeat, now) {
			live = append(live, e.claim)
		} else {
			l.dropClaim(e, now, true)
		}
	}
	return live
}

// ReleaseFilter names a runtime's own claims; empty fields match any.
type ReleaseFilter struct {
	Runtime, TabID, Project, Board string
	AnyTab, AnyBoard               bool
}

// ReleaseClaims frees the claims that match and returns them.
func (l *Library) ReleaseClaims(f ReleaseFilter, now int64) []Claim {
	var out []Claim
	for _, e := range l.scanClaims(f.Project) {
		c := e.claim
		if c.Owner.Runtime != f.Runtime || (!f.AnyTab && c.Owner.TabID != f.TabID) || (!f.AnyBoard && c.Board != f.Board) {
			continue
		}
		if l.dropClaim(e, now, false) {
			out = append(out, c)
		}
	}
	return out
}

func (l *Library) refreshClaim(e claimEntry, title string, now int64) bool {
	done := false
	_ = l.serialized(e.claim.Project, func() error {
		dir, _ := l.openProjectClaims(e.claim.Project, false)
		if dir == nil {
			return nil
		}
		defer dir.Close()
		cur, ok := readClaimFile(dir, e.name)
		if !ok || !sameClaim(cur, e.claim) {
			return nil
		}
		cur.Owner.ChatTitle, cur.Heartbeat = title, now
		done = overwrite(dir, cur) == nil
		return nil
	})
	return done
}

// ChatView is what the runtime knows of a chat.
type ChatView struct{ State, Title string }

// claimReleasing are the states of a chat between turns or gone; unknown is a restart that has not looked yet.
var claimReleasing = map[string]bool{"idle": true, "ready-for-review": true, "inactive": true, "cancelled": true}

// SweepResult counts what one pass did.
type SweepResult struct {
	Released           []Claim
	Refreshed, Cleared int
}

// SweepClaims is the owning runtime's pass over the claims: let go of those whose chat ended its turn or was closed,
// keep the others fresh, clear anyone's claim nobody refreshes any more. missing remembers, between passes, since
// when a claim's chat has been missing.
func (l *Library) SweepClaims(now int64, runtime string, chat func(tabID string) (ChatView, bool), missing map[string]int64) SweepResult {
	var res SweepResult
	seen := map[string]bool{}
	for _, e := range l.scanClaims("") {
		c := e.claim
		key := c.Project + "/" + e.name
		seen[key] = true
		if c.Owner.Runtime != runtime {
			if !IsClaimLive(c.Heartbeat, now) && l.dropClaim(e, now, true) {
				res.Cleared++
			}
			continue
		}
		view, ok := chat(c.Owner.TabID)
		title := ""
		if ok {
			if t, has := CleanText(view.Title, 120); has {
				title = t
			} else {
				title = locale.Pick("Agent", "Агент")
			}
		}
		if !ok {
			since, had := missing[key]
			if !had {
				since = now
				missing[key] = since
			}
			if now-since >= claimGoneMs && l.dropClaim(e, now, false) {
				res.Released = append(res.Released, c)
			}
			continue
		}
		delete(missing, key)
		if claimReleasing[view.State] && now-c.Since >= claimGraceMs {
			if l.dropClaim(e, now, false) {
				res.Released = append(res.Released, c)
			}
		} else if now-c.Heartbeat >= claimHeartbeatMs || title != c.Owner.ChatTitle {
			if l.refreshClaim(e, title, now) {
				res.Refreshed++
			}
		}
	}
	for k := range missing {
		if !seen[k] {
			delete(missing, k)
		}
	}
	return res
}
