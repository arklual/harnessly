package hlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// harnessly app: versions, channels, the team and links (internal/apps channels.go, team.go, guest.go) and the evals
// (eval.go). Publishing, promoting and the rest go through the runtime; eval runs in this process, on the person's own
// agent login, and mock-mcp is the server an eval's package asks for in place of its real one.

// sharedInstall says whether `app install` is one of the shared kinds: from a channel, or for the team.
func sharedInstall(args []string) bool {
	for _, a := range args {
		if a == "--channel" || a == "--team" || a == "--space" || strings.HasPrefix(a, "--channel=") {
			return true
		}
	}
	return false
}

// shareError is a refusal in the runtime's own words.
func shareError(status int, raw []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.Message != "" && status != 403 {
		return fail("%s", e.Message)
	}
	if status == 403 && e.Error == "not-owner" {
		return fail("only an owner of the team does this")
	}
	return apiError(status, raw)
}

// appFlags reads --name value / --name=value pairs and the bare words; booleans lists the flags that take no value.
func appFlags(args []string, booleans ...string) (map[string]string, []string, error) {
	vals := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		isBool := false
		for _, b := range booleans {
			isBool = isBool || b == name
		}
		switch {
		case isBool && !hasVal:
			vals[name] = "true"
		case hasVal:
			vals[name] = val
		case i+1 < len(args):
			i++
			vals[name] = args[i]
		default:
			return nil, nil, usageErr("--%s needs a value", name)
		}
	}
	return vals, rest, nil
}

func allowFlags(vals map[string]string, allowed ...string) error {
	for name := range vals {
		ok := false
		for _, a := range allowed {
			ok = ok || a == name
		}
		if !ok {
			return usageErr("unknown flag --%s", name)
		}
	}
	return nil
}

// appIDIn is the id of the app in a folder ("" is the working folder), or the --app flag.
func (c *cli) appIDIn(flag, dir string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	pkg, ps := apps.LoadPackage(c.appDir(dir))
	if pkg == nil {
		return "", fail("no app here: %s (name it with --app ID)", ps.Error())
	}
	return pkg.Manifest.ID, nil
}

func parseChannel(s string) (string, error) {
	if _, ok := apps.ParseChannel(s); !ok {
		return "", usageErr("--channel is dev, beta or stable")
	}
	return s, nil
}

func (c *cli) appPublish(args []string) error {
	vals, rest, err := appFlags(args, "team")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "channel", "team"); err != nil {
		return err
	}
	if len(rest) > 1 {
		return usageErr("app publish takes one folder")
	}
	channel := vals["channel"]
	if channel == "" {
		channel = "dev"
	}
	if _, err := parseChannel(channel); err != nil {
		return err
	}
	dir := ""
	if len(rest) == 1 {
		dir = rest[0]
	}
	dir = c.appDir(dir)
	status, raw, err := c.call("POST", "/api/cli/apps/publish", nil, map[string]any{"dir": dir, "channel": channel, "team": vals["team"] == "true"})
	if err != nil {
		return err
	}
	var out struct {
		Error     string        `json:"error"`
		ID        string        `json:"id"`
		Version   string        `json:"version"`
		TeamError string        `json:"teamError"`
		Problems  apps.Problems `json:"problems"`
	}
	_ = json.Unmarshal(raw, &out)
	if status == 422 && out.Error == "invalid" {
		for _, p := range out.Problems {
			c.out("%s\n", p)
		}
		return fail("not published: the package has errors (harnessly app validate %s)", dir)
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
	} else {
		c.out("published %s %s on %s (a version is never changed or reused)\n", out.ID, out.Version, channel)
		if vals["team"] == "true" {
			if out.TeamError != "" {
				c.out("not in the team's catalog: %s\n", out.TeamError)
			} else {
				c.out("in the team's catalog; an owner installs it for the team: harnessly app install --team %s --channel %s\n", out.ID, channel)
			}
		}
	}
	if out.TeamError != "" {
		return exitCode(exitError)
	}
	return nil
}

func (c *cli) appPromote(args []string) error {
	vals, rest, err := appFlags(args, "team")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "channel", "app", "threshold", "team"); err != nil {
		return err
	}
	if len(rest) < 1 || len(rest) > 2 {
		return usageErr("app promote VERSION --channel beta|stable [FOLDER]")
	}
	if _, err := parseChannel(vals["channel"]); err != nil {
		return err
	}
	dir := ""
	if len(rest) == 2 {
		dir = rest[1]
	}
	id, err := c.appIDIn(vals["app"], dir)
	if err != nil {
		return err
	}
	body := map[string]any{"id": id, "version": rest[0], "channel": vals["channel"], "team": vals["team"] == "true"}
	if t := vals["threshold"]; t != "" {
		f, err := strconv.ParseFloat(t, 64)
		if err != nil || f < 0 || f > 1 {
			return usageErr("--threshold is a share from 0 to 1")
		}
		body["threshold"] = f
	}
	status, raw, err := c.call("POST", "/api/cli/apps/promote", nil, body)
	if err != nil {
		return err
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &out)
	c.out("%s %s is on %s\n", id, out.Version, vals["channel"])
	return nil
}

func (c *cli) appRollback(args []string) error {
	vals, rest, err := appFlags(args, "team")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "channel", "app", "team"); err != nil {
		return err
	}
	if len(rest) > 1 {
		return usageErr("app rollback takes one folder")
	}
	if _, err := parseChannel(vals["channel"]); err != nil {
		return err
	}
	dir := ""
	if len(rest) == 1 {
		dir = rest[0]
	}
	id, err := c.appIDIn(vals["app"], dir)
	if err != nil {
		return err
	}
	status, raw, err := c.call("POST", "/api/cli/apps/rollback", nil, map[string]any{"id": id, "channel": vals["channel"], "team": vals["team"] == "true"})
	if err != nil {
		return err
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &out)
	c.out("%s on %s is back at %s\n", id, vals["channel"], out.Version)
	return nil
}

func (c *cli) appChannels(args []string) error {
	if len(args) > 1 {
		return usageErr("app channels takes one app id")
	}
	q := url.Values{}
	if len(args) == 1 {
		q.Set("id", args[0])
	}
	raw, err := c.request("GET", "/api/cli/apps/channels", q, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		ID       string                                 `json:"id"`
		Channels map[string]apps.ChannelInfo            `json:"channels"`
		Apps     map[string]map[string]apps.ChannelInfo `json:"apps"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	show := func(id string, chs map[string]apps.ChannelInfo) {
		for _, name := range []string{"dev", "beta", "stable"} {
			if info, ok := chs[name]; ok {
				prev := ""
				if info.Previous != "" {
					prev = "\t(before: " + info.Previous + ")"
				}
				c.out("%s\t%s\t%s%s\n", id, name, info.Current, prev)
			}
		}
	}
	if out.ID != "" {
		show(out.ID, out.Channels)
	}
	for id, chs := range out.Apps {
		show(id, chs)
	}
	return nil
}

// appInstallShared is `app install --channel C ID` (follow a channel of a published app) and `app install --team ID
// [--channel C]` (an owner installs the catalog's app for the team).
func (c *cli) appInstallShared(args []string) error {
	vals, rest, err := appFlags(args, "team")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "channel", "team", "space"); err != nil {
		return err
	}
	if len(rest) != 1 {
		return usageErr("app install --channel stable ID, or app install --team ID [--channel stable]")
	}
	id := rest[0]
	team := vals["team"] == "true" || vals["space"] != ""
	channel := vals["channel"]
	if channel == "" {
		if !team {
			return usageErr("--channel dev, beta or stable")
		}
		channel = "stable"
	}
	if _, err := parseChannel(channel); err != nil {
		return err
	}
	path := "/api/cli/apps/follow"
	if team {
		path = "/api/cli/apps/team/install"
	}
	status, raw, err := c.call("POST", path, nil, map[string]any{"id": id, "channel": channel})
	if err != nil {
		return err
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if team {
		c.out("%s is installed for the team on %s; the members have it without installing\n", id, channel)
	} else {
		c.out("installed %s, following %s (harnessly app update brings it to what the channel points at)\n", id, channel)
	}
	return nil
}

func (c *cli) appUpdate(args []string) error {
	vals, rest, err := appFlags(args, "yes")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "yes"); err != nil {
		return err
	}
	var confirm []string
	switch {
	case len(rest) == 1:
		confirm = nil
		if vals["yes"] == "true" {
			confirm = []string{rest[0]}
		}
	case len(rest) == 0 && vals["yes"] == "true":
		confirm = []string{"*"}
	case len(rest) > 1:
		return usageErr("app update takes one app id")
	}
	raw, err := c.request("POST", "/api/cli/apps/sync", nil, map[string]any{"confirm": confirm})
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Moved []apps.SyncResult `json:"moved"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, m := range out.Moved {
		if len(rest) == 1 && m.ID != rest[0] {
			continue
		}
		if m.Applied {
			c.out("%s: %s → %s\n", m.ID, m.From, m.To)
		} else {
			c.out("%s: %s → %s waits for you (%s); confirm with: harnessly app update --yes %s\n", m.ID, m.From, m.To, m.Reason, m.ID)
		}
	}
	if len(out.Moved) == 0 {
		c.out("everything follows its channel\n")
	}
	return nil
}

func parseTTL(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days <= 0 {
			return 0, usageErr("--ttl is like 12h or 7d")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, usageErr("--ttl is like 12h or 7d")
	}
	return d, nil
}

func (c *cli) appShare(args []string) error {
	vals, rest, err := appFlags(args, "write")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "view", "ttl", "write"); err != nil {
		return err
	}
	if len(rest) != 1 {
		return usageErr("app share ID [--view main] [--ttl 7d] [--write]")
	}
	ttl, err := parseTTL(vals["ttl"])
	if err != nil {
		return err
	}
	status, raw, err := c.call("POST", "/api/cli/apps/share", nil, map[string]any{
		"id": rest[0], "view": vals["view"], "ttlSeconds": int64(ttl / time.Second), "write": vals["write"] == "true",
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Path string `json:"path"`
		Link struct {
			ID        string `json:"id"`
			ExpiresAt int64  `json:"expiresAt"`
			Write     bool   `json:"write"`
		} `json:"link"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	mode := "reads only"
	if out.Link.Write {
		mode = "can write to its own storage"
	}
	c.out("%s\nthis machine's address followed by that path opens the app's view for a guest, with no account, no chat and no agent\n"+
		"it %s and ends %s; end it earlier: harnessly app unshare %s\n",
		out.Path, mode, time.UnixMilli(out.Link.ExpiresAt).Format("2006-01-02 15:04"), out.Link.ID)
	return nil
}

func (c *cli) appShares(args []string) error {
	q := url.Values{}
	if len(args) == 1 {
		q.Set("id", args[0])
	} else if len(args) > 1 {
		return usageErr("app shares takes one app id")
	}
	raw, err := c.request("GET", "/api/cli/apps/shares", q, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Links []struct {
			ID        string `json:"id"`
			App       string `json:"app"`
			View      string `json:"view"`
			ExpiresAt int64  `json:"expiresAt"`
			Write     bool   `json:"write"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, l := range out.Links {
		c.out("%s\t%s/%s\tends %s\n", l.ID, l.App, l.View, time.UnixMilli(l.ExpiresAt).Format("2006-01-02 15:04"))
	}
	if len(out.Links) == 0 {
		c.out("no links\n")
	}
	return nil
}

func (c *cli) appUnshare(args []string) error {
	if len(args) != 1 {
		return usageErr("app unshare takes the link's id (harnessly app shares)")
	}
	status, raw, err := c.call("POST", "/api/cli/apps/unshare", nil, map[string]string{"id": args[0]})
	if err != nil {
		return err
	}
	if status >= 300 {
		return shareError(status, raw)
	}
	if !c.json {
		c.out("the link %s is closed\n", args[0])
	} else {
		c.printRaw(raw)
	}
	return nil
}

// appTeam: harnessly app team [list | set SPACE ROLE | publish ID VERSION --channel C | uninstall ID].
func (c *cli) appTeam(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		raw, err := c.request("GET", "/api/cli/apps/team", nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var out apps.TeamInfo
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if out.Space == "" {
			c.out("this machine is in no team\n")
			return nil
		}
		c.out("team %s, you are %s\n", out.Space, out.Role)
		for _, e := range out.Catalog {
			state := ""
			if e.Installed {
				state = "\tinstalled for the team at " + e.At
			}
			c.out("%s\t%s\t%s\t%s%s\n", e.ID, e.Channel, e.Version, e.Name, state)
		}
		return nil
	case "set":
		if len(args) != 2 {
			return usageErr("app team set SPACE owner|member  (a machine without the platform's word; with none: set \"\" \"\")")
		}
		raw, err := c.request("POST", "/api/cli/apps/team/set", nil, map[string]string{"space": args[0], "role": args[1]})
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
		}
		return nil
	case "publish":
		vals, rest, err := appFlags(args)
		if err != nil {
			return err
		}
		if err := allowFlags(vals, "channel"); err != nil {
			return err
		}
		if len(rest) != 2 {
			return usageErr("app team publish ID VERSION --channel stable")
		}
		if _, err := parseChannel(vals["channel"]); err != nil {
			return err
		}
		status, raw, err := c.call("POST", "/api/cli/apps/team/publish", nil, map[string]string{"id": rest[0], "version": rest[1], "channel": vals["channel"]})
		if err != nil {
			return err
		}
		if status >= 300 {
			return shareError(status, raw)
		}
		if !c.json {
			c.out("%s %s is in the team's catalog on %s\n", rest[0], rest[1], vals["channel"])
		} else {
			c.printRaw(raw)
		}
		return nil
	case "uninstall":
		if len(args) != 1 {
			return usageErr("app team uninstall ID")
		}
		status, raw, err := c.call("POST", "/api/cli/apps/team/uninstall", nil, map[string]string{"id": args[0]})
		if err != nil {
			return err
		}
		if status >= 300 {
			return shareError(status, raw)
		}
		if !c.json {
			c.out("%s is off the team's machines\n", args[0])
		} else {
			c.printRaw(raw)
		}
		return nil
	}
	return usageErr("app team takes list, set, publish or uninstall")
}

// appMockMCP is the stdio MCP server an eval's package runs in place of its real one.
func (c *cli) appMockMCP(args []string) error {
	if len(args) != 1 {
		return usageErr("app mock-mcp takes one fixture file")
	}
	f, err := apps.ReadMockFixture(args[0])
	if err != nil {
		return fail("%v", err)
	}
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	return apps.ServeMockMCP(in, c.env.Stdout, f)
}

// appEval runs the scenarios of a package on a real agent: harnessly app eval [DIR] [--agent claude] [--runs N]
// [--case NAME] [--threshold 0.8] [--model M] [--no-baseline] [--no-judge] [--no-report]. The exit code is 1 when the
// pass rate is under the threshold, so CI can gate on it.
func (c *cli) appEval(args []string) error {
	vals, rest, err := appFlags(args, "no-baseline", "no-judge", "no-report")
	if err != nil {
		return err
	}
	if err := allowFlags(vals, "agent", "runs", "case", "threshold", "model", "timeout", "no-baseline", "no-judge", "no-report"); err != nil {
		return err
	}
	if len(rest) > 1 {
		return usageErr("app eval takes one folder")
	}
	dir := ""
	if len(rest) == 1 {
		dir = rest[0]
	}
	dir = c.appDir(dir)

	opt := apps.EvalOptions{NoBaseline: vals["no-baseline"] == "true"}
	switch agent := vals["agent"]; agent {
	case "", "claude":
		opt.Agent = &apps.ClaudeEval{Model: vals["model"]}
		if vals["no-judge"] != "true" {
			opt.Judge = &apps.ClaudeJudge{Model: vals["model"]}
		}
	case "codex":
		return fail("--agent codex is not supported yet: the eval runs Claude Code in print mode")
	default:
		return usageErr("--agent is claude")
	}
	if v := vals["runs"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10 {
			return usageErr("--runs is 1 to 10")
		}
		opt.RunsOverride = n
	}
	if v := vals["threshold"]; v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 1 {
			return usageErr("--threshold is a share above 0, up to 1")
		}
		opt.Threshold = f
	}
	if v := vals["timeout"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return usageErr("--timeout is like 5m")
		}
		opt.Timeout = d
	}
	if v := vals["case"]; v != "" {
		opt.Only = strings.Split(v, ",")
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	opt.Prepare = func(d string, ec apps.EvalCase) (string, func(), error) { return apps.PrepareEvalPackage(d, ec, bin) }

	rep, err := apps.RunEval(context.Background(), dir, opt)
	if err != nil {
		return fail("%v", err)
	}
	if vals["no-report"] != "true" {
		if err := apps.WriteEvalReport(dir, rep); err != nil {
			return fail("the report was not written: %v", err)
		}
	}
	if c.json {
		raw, _ := json.Marshal(rep)
		c.printRaw(raw)
	} else {
		c.printEval(rep)
	}
	if !rep.Ok {
		return exitCode(exitError)
	}
	return nil
}

func (c *cli) printEval(rep apps.EvalReport) {
	for _, cr := range rep.Cases {
		mark := "pass"
		if cr.Passed < cr.Runs {
			mark = "FAIL"
		}
		c.out("%s\t%s\t%d/%d", mark, cr.Name, cr.Passed, cr.Runs)
		if cr.BaselineRuns > 0 {
			c.out("\twithout the app %d/%d", cr.BaselinePassed, cr.BaselineRuns)
		}
		if cr.Unjudged > 0 {
			c.out("\t%d criteria nobody judged (--no-judge)", cr.Unjudged)
		}
		c.out("\n")
		for why, n := range cr.Failures {
			c.out("\t%d× %s\n", n, why)
		}
		for _, e := range cr.Errors {
			c.out("\terror: %s\n", e)
		}
		for _, n := range cr.Notes {
			c.out("\tnote: %s\n", n)
		}
	}
	c.out("%d of %d runs passed (%.0f%%, needs %.0f%%): %s\n", rep.Passed, rep.Runs, rep.PassRate*100, rep.Threshold*100, map[bool]string{true: "ok", false: "not ok"}[rep.Ok])
	if rep.BaselineRate != nil {
		c.out("without the app: %.0f%%\n", *rep.BaselineRate*100)
	}
	if rep.ContextTokens != nil {
		c.out("the app adds %s tokens to what a run reads\n", fmt.Sprint(*rep.ContextTokens))
	}
}
