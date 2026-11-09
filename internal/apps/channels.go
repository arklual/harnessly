package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Versions and channels of a published app. A version is immutable and its number is never used twice; a channel is a
// pointer at one of the versions, with the ones it pointed at before, so a rollback is moving the pointer back.
//
//	dev → beta → stable
//
// Published versions live under <apps>/published/<id>/v/<version>/ (a copy of the package, its manifest saying that
// version), the pointers in <apps>/published/<id>/channels.json. Installs follow a channel (Registry.InstallChannel,
// Registry.SyncChannels).

// Channel is a track a version is published on.
type Channel string

const (
	ChannelDev    Channel = "dev"
	ChannelBeta   Channel = "beta"
	ChannelStable Channel = "stable"
)

// ParseChannel reads a channel name.
func ParseChannel(s string) (Channel, bool) {
	switch c := Channel(s); c {
	case ChannelDev, ChannelBeta, ChannelStable:
		return c, true
	}
	return "", false
}

// DefaultEvalThreshold is the share of passed evals a version needs to go to stable.
const DefaultEvalThreshold = 0.8

// EvalReportPath is where `harnessly app eval` leaves its report in the package; it travels with the published copy.
const EvalReportPath = ".harnessly/eval.json"

var (
	// ErrVersionExists: the version is published; a number is never used twice.
	ErrVersionExists = errors.New("apps: this version is already published")
	// ErrChannelVersion: the manifest's version does not suit the channel.
	ErrChannelVersion = errors.New("apps: the version does not suit the channel")
	// ErrNoVersion: nothing is published under that name.
	ErrNoVersion = errors.New("apps: no such published version")
	// ErrNoChannel: the channel has no version yet.
	ErrNoChannel = errors.New("apps: nothing is published on this channel")
	// ErrNoPrevious: the channel has no earlier version to go back to.
	ErrNoPrevious = errors.New("apps: the channel has no earlier version")
	// ErrEvalGate: the evals do not let the version go to stable.
	ErrEvalGate = errors.New("apps: the evals do not pass")
)

type channelPointer struct {
	Current string `json:"current"`
	// History ends with Current.
	History []string `json:"history"`
}

type channelsFile struct {
	Channels map[string]*channelPointer `json:"channels"`
	// Versions are the published ones with the time they were published at (ms).
	Versions map[string]int64 `json:"versions"`
}

func (r *Registry) publishedDir(id string) string { return filepath.Join(r.Root, "published", id) }

// VersionDir is the folder of a published version.
func (r *Registry) VersionDir(id, version string) string {
	return filepath.Join(r.publishedDir(id), "v", version)
}

func (r *Registry) readChannels(id string) (channelsFile, error) {
	out := channelsFile{Channels: map[string]*channelPointer{}, Versions: map[string]int64{}}
	raw, err := os.ReadFile(filepath.Join(r.publishedDir(id), "channels.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Channels == nil {
		out.Channels = map[string]*channelPointer{}
	}
	if out.Versions == nil {
		out.Versions = map[string]int64{}
	}
	return out, nil
}

func (r *Registry) writeChannels(id string, c channelsFile) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.publishedDir(id), "channels.json"), append(raw, '\n'))
}

func (c *channelsFile) point(ch Channel, version string) {
	p := c.Channels[string(ch)]
	if p == nil {
		p = &channelPointer{}
		c.Channels[string(ch)] = p
	}
	if p.Current != version {
		p.History = append(p.History, version)
	}
	p.Current = version
}

// splitVersion is "1.3.0-beta.2" as "1.3.0" and "beta.2".
func splitVersion(v string) (base, pre string) {
	base, pre, _ = strings.Cut(v, "-")
	return base, pre
}

func majorOf(v string) int {
	base, _ := splitVersion(v)
	n, _, err := parseVersion(base)
	if err != nil {
		return -1
	}
	return n[0]
}

// PublishOptions say how to publish.
type PublishOptions struct {
	Now func() time.Time
}

func (o PublishOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// PublishedVersion is a version in the published store.
type PublishedVersion struct {
	ID       string    `json:"id"`
	Version  string    `json:"version"`
	Channel  Channel   `json:"channel"`
	Dir      string    `json:"dir"`
	Manifest *Manifest `json:"-"`
}

// nextPre is the number after the highest "<channel>.N" the app has under that base.
func nextPre(versions map[string]int64, base string, ch Channel) int {
	n := 0
	for v := range versions {
		b, pre := splitVersion(v)
		rest, ok := strings.CutPrefix(pre, string(ch)+".")
		if b != base || !ok {
			continue
		}
		if k, err := strconv.Atoi(rest); err == nil && k > n {
			n = k
		}
	}
	return n + 1
}

// Publish puts the package in dir on a channel as a new immutable version. A package whose manifest says 1.3.0 goes to
// stable as 1.3.0, and to dev or beta as 1.3.0-dev.N or 1.3.0-beta.N (the next free N); a manifest that already says
// a pre-release is published as written, on dev or beta only.
func (r *Registry) Publish(dir string, ch Channel, opt PublishOptions) (PublishedVersion, Problems, error) {
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		return PublishedVersion{}, ps, ErrInvalid
	}
	if bad := archiveNameProblems(pkg.Dir); len(bad) > 0 {
		return PublishedVersion{}, ps, fmt.Errorf("%w: %s", ErrArchive, strings.Join(bad, "; "))
	}
	m := pkg.Manifest
	base, pre := splitVersion(m.Version)

	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.readChannels(m.ID)
	if err != nil {
		return PublishedVersion{}, ps, err
	}
	version := m.Version
	switch {
	case ch == ChannelStable && pre != "":
		return PublishedVersion{}, ps, fmt.Errorf("%w: %s is a pre-release, stable takes %s", ErrChannelVersion, m.Version, base)
	case ch != ChannelStable && pre == "":
		version = fmt.Sprintf("%s-%s.%d", base, ch, nextPre(state.Versions, base, ch))
	}
	if _, taken := state.Versions[version]; taken {
		return PublishedVersion{}, ps, fmt.Errorf("%w: %s %s (raise the version in the manifest)", ErrVersionExists, m.ID, version)
	}
	target := r.VersionDir(m.ID, version)
	if err := copyPackage(pkg.Dir, target); err != nil {
		return PublishedVersion{}, ps, err
	}
	if err := setManifestVersion(target, version); err != nil {
		_ = os.RemoveAll(target)
		return PublishedVersion{}, ps, err
	}
	state.Versions[version] = opt.now().UnixMilli()
	state.point(ch, version)
	if err := r.writeChannels(m.ID, state); err != nil {
		_ = os.RemoveAll(target)
		return PublishedVersion{}, ps, err
	}
	out := PublishedVersion{ID: m.ID, Version: version, Channel: ch, Dir: target}
	return out, ps, nil
}

// setManifestVersion rewrites the version of the manifest in a package folder.
func setManifestVersion(dir, version string) error {
	file := filepath.Join(dir, ManifestPath)
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if cur := string(doc["version"]); cur == strconv.Quote(version) {
		return nil
	}
	doc["version"], _ = json.Marshal(version)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(file, append(out, '\n'))
}

// resolveVersion finds a published version by what a person typed: the exact version, or a version without its
// pre-release (1.3.0), which stands for the latest published one with that base.
func resolveVersion(state channelsFile, ref string) (string, bool) {
	if _, ok := state.Versions[ref]; ok {
		return ref, true
	}
	best, at := "", int64(-1)
	for v, t := range state.Versions {
		if b, _ := splitVersion(v); b == ref && t >= at {
			if t > at || v > best {
				best, at = v, t
			}
		}
	}
	return best, best != ""
}

// PromoteOptions say how to promote.
type PromoteOptions struct {
	// EvalThreshold is the share of evals that must pass for stable; 0 is DefaultEvalThreshold.
	EvalThreshold float64
	Now           func() time.Time
}

// evalGate says whether the version in dir may go to stable: a package with evals needs a report with a pass rate at
// the threshold; a package with none has nothing to ask.
func evalGate(dir string, threshold float64) error {
	if threshold <= 0 {
		threshold = DefaultEvalThreshold
	}
	cases, _ := filepath.Glob(filepath.Join(dir, "evals", "*.json"))
	if len(cases) == 0 {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(EvalReportPath)))
	if err != nil {
		return fmt.Errorf("%w: the package has evals and no report (harnessly app eval first)", ErrEvalGate)
	}
	var rep struct {
		PassRate float64 `json:"passRate"`
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fmt.Errorf("%w: the report is unreadable", ErrEvalGate)
	}
	if rep.PassRate < threshold {
		return fmt.Errorf("%w: %.0f%% passed, stable needs %.0f%%", ErrEvalGate, rep.PassRate*100, threshold*100)
	}
	return nil
}

// Promote moves a published version up to beta or stable. The version number of the target channel is made from the
// source: stable takes the base (1.3.0-beta.2 becomes 1.3.0, copied as a version of its own), beta takes the next
// 1.3.0-beta.N from a dev version. Stable asks the evals (EvalThreshold).
func (r *Registry) Promote(id, ref string, to Channel, opt PromoteOptions) (PublishedVersion, error) {
	if to != ChannelBeta && to != ChannelStable {
		return PublishedVersion{}, fmt.Errorf("%w: promote goes to beta or stable", ErrChannelVersion)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.readChannels(id)
	if err != nil {
		return PublishedVersion{}, err
	}
	src, ok := resolveVersion(state, ref)
	if !ok {
		return PublishedVersion{}, fmt.Errorf("%w: %s %s", ErrNoVersion, id, ref)
	}
	base, pre := splitVersion(src)
	target := src
	switch {
	case to == ChannelStable:
		target = base
	case !strings.HasPrefix(pre, "beta."):
		target = fmt.Sprintf("%s-beta.%d", base, nextPre(state.Versions, base, ChannelBeta))
	}
	if to == ChannelStable {
		if err := evalGate(r.VersionDir(id, src), opt.EvalThreshold); err != nil {
			return PublishedVersion{}, err
		}
	}
	dir := r.VersionDir(id, target)
	if _, taken := state.Versions[target]; !taken {
		if err := copyPackage(r.VersionDir(id, src), dir); err != nil {
			return PublishedVersion{}, err
		}
		if err := setManifestVersion(dir, target); err != nil {
			_ = os.RemoveAll(dir)
			return PublishedVersion{}, err
		}
		now := time.Now
		if opt.Now != nil {
			now = opt.Now
		}
		state.Versions[target] = now().UnixMilli()
	}
	state.point(to, target)
	if err := r.writeChannels(id, state); err != nil {
		return PublishedVersion{}, err
	}
	return PublishedVersion{ID: id, Version: target, Channel: to, Dir: dir}, nil
}

// Rollback moves the channel back to the version it pointed at before. The version it leaves stays published, and its
// number stays taken.
func (r *Registry) Rollback(id string, ch Channel) (PublishedVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.readChannels(id)
	if err != nil {
		return PublishedVersion{}, err
	}
	p := state.Channels[string(ch)]
	if p == nil || p.Current == "" {
		return PublishedVersion{}, fmt.Errorf("%w: %s %s", ErrNoChannel, id, ch)
	}
	if len(p.History) < 2 {
		return PublishedVersion{}, fmt.Errorf("%w: %s %s", ErrNoPrevious, id, ch)
	}
	p.History = p.History[:len(p.History)-1]
	p.Current = p.History[len(p.History)-1]
	if err := r.writeChannels(id, state); err != nil {
		return PublishedVersion{}, err
	}
	return PublishedVersion{ID: id, Version: p.Current, Channel: ch, Dir: r.VersionDir(id, p.Current)}, nil
}

// ChannelInfo is where a channel points and where it pointed before.
type ChannelInfo struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}

// Channels are the channels of a published app that have a version.
func (r *Registry) Channels(id string) (map[Channel]ChannelInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.readChannels(id)
	if err != nil {
		return nil, err
	}
	out := map[Channel]ChannelInfo{}
	for name, p := range state.Channels {
		if p == nil || p.Current == "" {
			continue
		}
		info := ChannelInfo{Current: p.Current}
		if len(p.History) >= 2 {
			info.Previous = p.History[len(p.History)-2]
		}
		out[Channel(name)] = info
	}
	return out, nil
}

// Published lists the apps that have published versions, with their channels.
func (r *Registry) Published() (map[string]map[Channel]ChannelInfo, error) {
	entries, err := os.ReadDir(filepath.Join(r.Root, "published"))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]map[Channel]ChannelInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]map[Channel]ChannelInfo{}
	for _, e := range entries {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		chs, err := r.Channels(e.Name())
		if err != nil {
			return nil, err
		}
		if len(chs) > 0 {
			out[e.Name()] = chs
		}
	}
	return out, nil
}

// ChannelVersion is the version a channel points at now.
func (r *Registry) ChannelVersion(id string, ch Channel) (string, error) {
	chs, err := r.Channels(id)
	if err != nil {
		return "", err
	}
	info, ok := chs[ch]
	if !ok {
		return "", fmt.Errorf("%w: %s %s", ErrNoChannel, id, ch)
	}
	return info.Current, nil
}

// InstallChannel installs the version a channel points at and makes the install follow that channel.
func (r *Registry) InstallChannel(id string, ch Channel, opt InstallOptions) (Installed, Problems, error) {
	version, err := r.ChannelVersion(id, ch)
	if err != nil {
		return Installed{}, nil, err
	}
	opt.Channel = string(ch)
	opt.Dev = false
	opt.Signed = r.versionSigned(id, version)
	return r.Install(r.VersionDir(id, version), opt)
}

// SyncResult is what following a channel did for one install.
type SyncResult struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	// Applied: the install is now at To. Otherwise Reason says what waits for a person's word: "major", "permissions".
	Applied bool   `json:"applied"`
	Reason  string `json:"reason,omitempty"`
}

// SyncOptions say how to follow the channels.
type SyncOptions struct {
	HostVersion string
	// Confirm applies the updates that wait for a word (a new major, new rights) of this app; "*" is every app.
	Confirm []string
}

// SyncChannels moves every install that follows a channel to the version the channel points at. A move back (the
// channel was rolled back, or the install's version is not in the channel's history any more) is made at once. A move
// up is made at once for a patch or a minor; a new major, or a new permission or secret, waits for a confirmation.
func (r *Registry) SyncChannels(opt SyncOptions) ([]SyncResult, error) {
	list, err := r.userInstalls()
	if err != nil {
		return nil, err
	}
	var out []SyncResult
	for _, a := range list {
		if a.Channel == "" || a.Dev {
			continue
		}
		ch, ok := ParseChannel(a.Channel)
		if !ok {
			continue
		}
		res, err := r.followOne(a, ch, opt)
		if err != nil {
			if errors.Is(err, ErrNoChannel) {
				continue
			}
			return out, err
		}
		if res.To != "" && res.To != res.From {
			out = append(out, res)
		}
	}
	return out, nil
}

func (r *Registry) followOne(a Installed, ch Channel, opt SyncOptions) (SyncResult, error) {
	r.mu.Lock()
	state, err := r.readChannels(a.ID)
	r.mu.Unlock()
	if err != nil {
		return SyncResult{}, err
	}
	p := state.Channels[string(ch)]
	if p == nil || p.Current == "" {
		return SyncResult{}, ErrNoChannel
	}
	res := SyncResult{ID: a.ID, From: a.Version, To: p.Current}
	if a.Version == p.Current {
		return res, nil
	}
	dir := r.VersionDir(a.ID, p.Current)
	if slices.Contains(p.History, a.Version) {
		// A step up: the install is behind the channel.
		next, _ := LoadPackage(dir)
		if next == nil {
			return res, fmt.Errorf("%w: %s %s", ErrNoVersion, a.ID, p.Current)
		}
		reason := ""
		switch {
		case majorOf(p.Current) != majorOf(a.Version):
			reason = "major"
		case a.Manifest != nil && grew(a.Manifest, next.Manifest):
			reason = "permissions"
		}
		if reason != "" && !slices.Contains(opt.Confirm, a.ID) && !slices.Contains(opt.Confirm, "*") {
			res.Reason = reason
			return res, nil
		}
	}
	if _, _, err := r.Install(dir, InstallOptions{HostVersion: opt.HostVersion, Channel: string(ch), Consent: ConsentAsk, Signed: r.versionSigned(a.ID, p.Current)}); err != nil {
		return res, err
	}
	res.Applied = true
	return res, nil
}

// grew: the new manifest asks for a permission, a secret or a network address the old one did not.
func grew(old, next *Manifest) bool {
	for _, p := range next.Permissions {
		if !slices.Contains(old.Permissions, p) {
			return true
		}
	}
	for _, s := range next.Secrets {
		if !slices.ContainsFunc(old.Secrets, func(o Secret) bool { return o.Name == s.Name }) {
			return true
		}
	}
	if next.Network != nil {
		var have []string
		if old.Network != nil {
			have = slices.Concat(old.Network.Connect, old.Network.Resources, old.Network.Frames)
		}
		for _, h := range slices.Concat(next.Network.Connect, next.Network.Resources, next.Network.Frames) {
			if !slices.Contains(have, h) {
				return true
			}
		}
	}
	return false
}
