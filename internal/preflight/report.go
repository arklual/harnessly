package preflight

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The thresholds of the hub's preflight report (docs/hub/api.md).
const (
	gb             = int64(1) << 30
	diskFail       = 3 * gb
	diskWarn       = 10 * gb
	memFail        = 2 * gb
	memWarn        = 1 * gb
	minNodeMajor   = 20
	clockWarnAfter = 60 * time.Second
)

// ReportOptions say what a report looks at beyond the tools.
type ReportOptions struct {
	// WorkDir is the folder whose filesystem the disk numbers are about (the runtime's work folder).
	WorkDir string
	// HubURL is the hub's public address; its Date header is the clock the machine is compared with. "": no clock check.
	HubURL string
	// HTTP fetches the hub's date (nil: http.DefaultClient).
	HTTP *http.Client
	// Now is the clock (nil: time.Now).
	Now func() time.Time

	// The next three are for tests: where /proc/meminfo, the cgroup and os-release live, and a disk probe.
	MeminfoFile, CgroupDir, OSReleaseFile string
	Disk                                  func(path string) (total, free int64, err error)
}

func (o ReportOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Report is the machine's preflight for the hub: what the system is, how much disk and memory it has, and which tools
// are there, with a verdict per concern. It looks again at the tools each time (the answer of Runtime is kept for 30 s).
func (c *Checker) Report(ctx context.Context, o ReportOptions) hubproto.PreflightReport {
	c.Invalidate()
	r := c.Runtime()
	path := c.shellPath()
	node := c.tool(path, "node", "--version")

	rep := hubproto.PreflightReport{
		At:     o.now().UnixMilli(),
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		CPUs:   runtime.NumCPU(),
		OSName: osName(o.OSReleaseFile),
	}
	rep.MemTotal, rep.MemAvailable = memory(o.MeminfoFile, o.CgroupDir)
	workDir := o.WorkDir
	if workDir == "" {
		workDir = c.Home
	}
	rep.Disk = hubproto.Disk{Path: workDir}
	diskProbe := o.Disk
	if diskProbe == nil {
		diskProbe = diskUsage
	}
	if total, free, err := diskProbe(nearestExisting(workDir)); err == nil {
		rep.Disk.Total, rep.Disk.Free = total, free
	}

	compatible := r.Tmux.Compatible
	rep.Tools = hubproto.Tools{
		Tmux:   hubproto.ToolState{Installed: r.Tmux.Installed, Version: r.Tmux.Version, Compatible: &compatible},
		Git:    hubproto.ToolState{Installed: r.Git.Installed, Version: r.Git.Version},
		Node:   hubproto.ToolState{Installed: node.Installed, Version: node.Version},
		Claude: hubproto.ToolState{Installed: r.Claude.Installed || r.Claude.BinaryPath != nil, Version: r.Claude.Version},
		Codex:  hubproto.ToolState{Installed: r.Codex.Installed, Version: r.Codex.Version},
	}
	rep.Checks = checks(rep, node)
	if skew, ok := clockSkew(ctx, o); ok {
		rep.Checks = append(rep.Checks, clockCheck(skew))
	}
	return rep
}

func checks(rep hubproto.PreflightReport, node Tool) []hubproto.Check {
	out := []hubproto.Check{diskCheck(rep.Disk), memoryCheck(rep.MemTotal, rep.MemAvailable)}

	tmux := hubproto.Check{ID: "tmux", Level: hubproto.LevelOK}
	switch {
	case !rep.Tools.Tmux.Installed:
		tmux.Level, tmux.Message = hubproto.LevelFail, "tmux не найден: без него не запустить агентов. Поставьте tmux 2.9 или новее"
	case rep.Tools.Tmux.Compatible != nil && !*rep.Tools.Tmux.Compatible:
		tmux.Level, tmux.Message = hubproto.LevelFail, "tmux "+versionText(rep.Tools.Tmux.Version)+" слишком старый: нужен 2.9 или новее"
	default:
		tmux.Message = "tmux " + versionText(rep.Tools.Tmux.Version)
	}

	git := hubproto.Check{ID: "git", Level: hubproto.LevelOK, Message: "git " + versionText(rep.Tools.Git.Version)}
	if !rep.Tools.Git.Installed {
		git.Level, git.Message = hubproto.LevelWarn, "git не найден: агентам он понадобится для работы с кодом"
	}

	nodeCheck := hubproto.Check{ID: "node", Level: hubproto.LevelOK, Message: "Node.js " + versionText(rep.Tools.Node.Version)}
	switch {
	case !rep.Tools.Node.Installed:
		nodeCheck.Level, nodeCheck.Message = hubproto.LevelWarn, "Node.js не найден. Codex не поставить без Node.js 20+"
	case !node.Uncertain && nodeMajor(rep.Tools.Node.Version) < minNodeMajor:
		nodeCheck.Level, nodeCheck.Message = hubproto.LevelWarn, "Node.js "+versionText(rep.Tools.Node.Version)+" слишком старый. Codex не поставить без Node.js 20+"
	}
	return append(out, tmux, git, nodeCheck)
}

func diskCheck(d hubproto.Disk) hubproto.Check {
	c := hubproto.Check{ID: "disk", Level: hubproto.LevelOK, Message: humanGB(d.Free) + " свободно"}
	switch {
	case d.Total == 0:
		c.Level, c.Message = hubproto.LevelWarn, "не удалось узнать, сколько места на диске"
	case d.Free < diskFail:
		c.Level, c.Message = hubproto.LevelFail, "Мало места на диске: "+humanGB(d.Free)+" свободно, нужно хотя бы 3 ГБ"
	case d.Free < diskWarn:
		c.Level, c.Message = hubproto.LevelWarn, "Места на диске немного: "+humanGB(d.Free)+" свободно, лучше иметь 10 ГБ"
	}
	return c
}

func memoryCheck(total, available int64) hubproto.Check {
	c := hubproto.Check{ID: "memory", Level: hubproto.LevelOK, Message: humanGB(total) + " памяти"}
	switch {
	case total == 0:
		c.Level, c.Message = hubproto.LevelWarn, "не удалось узнать объём памяти"
	case total < memFail:
		c.Level, c.Message = hubproto.LevelFail, "Мало памяти: "+humanGB(total)+", нужно хотя бы 2 ГБ"
	case available < memWarn:
		c.Level, c.Message = hubproto.LevelWarn, "Свободной памяти меньше 1 ГБ: сейчас доступно "+humanGB(available)
	}
	return c
}

func clockCheck(skew time.Duration) hubproto.Check {
	if skew < 0 {
		skew = -skew
	}
	if skew > clockWarnAfter {
		return hubproto.Check{ID: "clock", Level: hubproto.LevelWarn,
			Message: fmt.Sprintf("Часы на машине расходятся с hub на %d с: подключение и вход могут не работать", int(skew.Seconds()))}
	}
	return hubproto.Check{ID: "clock", Level: hubproto.LevelOK, Message: "Часы сходятся с hub"}
}

// humanGB is bytes as «412 ГБ»; under ten gigabytes with one decimal.
func humanGB(b int64) string {
	g := float64(b) / float64(gb)
	if g < 10 {
		return strings.Replace(strconv.FormatFloat(g, 'f', 1, 64), ".", ",", 1) + " ГБ"
	}
	return strconv.FormatInt(int64(g+0.5), 10) + " ГБ"
}

func versionText(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nodeMajor(v *string) int {
	if v == nil {
		return 0
	}
	major, _, _ := strings.Cut(*v, ".")
	n, _ := strconv.Atoi(major)
	return n
}

// nearestExisting is path, or its closest ancestor that exists: a work folder not made yet lives on its parent's disk.
func nearestExisting(path string) string {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil || filepath.Dir(p) == p {
			return p
		}
	}
}

// clockSkew is this machine's clock minus the hub's, from the Date header of the hub's /healthz. False when the hub
// cannot be asked or does not date its answers.
func clockSkew(ctx context.Context, o ReportOptions) (time.Duration, bool) {
	if o.HubURL == "" {
		return 0, false
	}
	client := o.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, strings.TrimRight(o.HubURL, "/")+"/healthz", nil)
	if err != nil {
		return 0, false
	}
	sent := o.now()
	res, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	_ = res.Body.Close()
	hub, err := http.ParseTime(res.Header.Get("Date"))
	if err != nil {
		return 0, false
	}
	// The Date is when the hub answered, about half the round trip after the request left.
	mid := sent.Add(o.now().Sub(sent) / 2)
	return mid.Sub(hub), true
}
