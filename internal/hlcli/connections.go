package hlcli

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The person's trackers, wikis and code hosts, connected in Settings → Connections (internal/connections), read through
// the runtime: an agent never holds their tokens. Reads only; connecting and removing are the person's.

// connReasons are the refusals in words an agent can act on.
var connReasons = map[string]string{
	"denied":          "the service refused the connection's credentials: ask the person to reconnect it in Settings → Connections",
	"not-found":       "no such connection, issue or page",
	"unreachable":     "the service did not answer; try again later",
	"unsupported":     "this connection cannot do that (a tracker has no pages, a wiki no issues, only GitLab has merge requests)",
	"unknown-product": "the link is not a product harnessly knows",
}

func (c *cli) connRefused(status int, raw []byte) error {
	var r struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &r)
	if why := connReasons[r.Error]; why != "" {
		return fail("refused: %s — %s", r.Error, why)
	}
	return apiError(status, raw)
}

// connGet is one read of the connection routes.
func (c *cli) connGet(path string, q url.Values) ([]byte, error) {
	status, raw, err := c.call(http.MethodGet, "/api/cli/connections"+path, q, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, c.connRefused(status, raw)
	}
	return raw, nil
}

type cliPerson struct {
	Name  string `json:"name"`
	Login string `json:"login"`
}

type cliConn struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Name         string     `json:"name"`
	BaseURL      string     `json:"baseUrl"`
	State        string     `json:"state"`
	Primary      bool       `json:"primary"`
	Env          bool       `json:"env"`
	Capabilities []string   `json:"capabilities"`
	Me           *cliPerson `json:"me"`
}

type cliIssue struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Status struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	} `json:"status"`
	Type     string     `json:"type"`
	Priority string     `json:"priority"`
	Project  string     `json:"project"`
	Assignee *cliPerson `json:"assignee"`
	Reporter *cliPerson `json:"reporter"`
	Parent   *struct {
		Key   string `json:"key"`
		Title string `json:"title"`
	} `json:"parent"`
	Labels      []string  `json:"labels"`
	Updated     time.Time `json:"updated"`
	Description string    `json:"description"`
	Comments    []struct {
		Author  *cliPerson `json:"author"`
		Text    string     `json:"text"`
		Created time.Time  `json:"created"`
	} `json:"comments"`
}

type cliPage struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	URL     string     `json:"url"`
	Space   string     `json:"space"`
	Excerpt string     `json:"excerpt"`
	Author  *cliPerson `json:"author"`
	Updated time.Time  `json:"updated"`
	Body    string     `json:"body"`
}

func (p *cliPerson) String() string {
	if p == nil {
		return ""
	}
	if p.Name != "" {
		return p.Name
	}
	return p.Login
}

func (c *cli) connList() ([]cliConn, []byte, error) {
	raw, err := c.connGet("", nil)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Connections []cliConn `json:"connections"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return out.Connections, raw, nil
}

// connections: harnessly connections — what is connected.
func (c *cli) connections(args []string) error {
	if len(args) > 0 && args[0] != "list" && args[0] != "ls" {
		return usageErr("connections takes no arguments (harnessly connections --help)")
	}
	list, raw, err := c.connList()
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if len(list) == 0 {
		c.out("nothing is connected: ask the person to connect their tracker or wiki in Settings → Connections\n")
		return nil
	}
	for _, x := range list {
		marks := []string{x.State}
		if x.Primary {
			marks = append(marks, "primary")
		}
		if x.Env {
			marks = append(marks, "deployment")
		}
		who := ""
		if x.Me != nil {
			who = "\tas " + x.Me.String()
		}
		c.out("%s\t%s\t%s\t%s\t%s\t%s%s\n", x.ID, x.Kind, x.Name, x.BaseURL, strings.Join(x.Capabilities, ","), strings.Join(marks, ","), who)
	}
	return nil
}

// primaryOf is the connection used for a capability when none is named: the primary one, else the first that has it.
func (c *cli) primaryOf(capability string) (string, error) {
	list, _, err := c.connList()
	if err != nil {
		return "", err
	}
	first := ""
	for _, x := range list {
		has := false
		for _, cp := range x.Capabilities {
			has = has || cp == capability
		}
		if !has {
			continue
		}
		if x.Primary {
			return x.ID, nil
		}
		if first == "" {
			first = x.ID
		}
	}
	if first == "" {
		what := "tracker"
		switch capability {
		case "pages":
			what = "wiki"
		case "code":
			what = "code host (GitLab)"
		}
		return "", fail("no %s is connected: ask the person to connect one in Settings → Connections", what)
	}
	return first, nil
}

// searchFlags reads [--conn ID] [--mine] [--open] [--project KEY] [--limit N] [text…].
func searchFlags(args []string, issues bool) (conn string, q url.Values, err error) {
	q = url.Values{}
	var text []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func(flag string) (string, error) {
			if v, ok := strings.CutPrefix(a, flag+"="); ok {
				return v, nil
			}
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", flag)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "--conn" || strings.HasPrefix(a, "--conn="):
			if conn, err = value("--conn"); err != nil {
				return
			}
		case a == "--limit" || strings.HasPrefix(a, "--limit="):
			var v string
			if v, err = value("--limit"); err != nil {
				return
			}
			if n, e := strconv.Atoi(v); e != nil || n < 1 {
				return "", nil, usageErr("--limit takes a number")
			}
			q.Set("limit", v)
		case issues && a == "--mine":
			q.Set("mine", "1")
		case issues && a == "--open":
			q.Set("open", "1")
		case issues && (a == "--project" || strings.HasPrefix(a, "--project=")):
			var v string
			if v, err = value("--project"); err != nil {
				return
			}
			q.Set("project", v)
		case issues && (a == "--native" || strings.HasPrefix(a, "--native=")):
			var v string
			if v, err = value("--native"); err != nil {
				return
			}
			q.Set("native", v)
		case strings.HasPrefix(a, "--"):
			return "", nil, usageErr("unknown flag %s", a)
		default:
			text = append(text, a)
		}
	}
	if t := strings.TrimSpace(strings.Join(text, " ")); t != "" {
		q.Set("q", t)
	}
	return conn, q, nil
}

// issues: harnessly issues [--conn ID] [--mine] [--open] [--project KEY] [text…].
func (c *cli) issues(args []string) error {
	conn, q, err := searchFlags(args, true)
	if err != nil {
		return err
	}
	if conn == "" {
		if conn, err = c.primaryOf("issues"); err != nil {
			return err
		}
	}
	raw, err := c.connGet("/"+url.PathEscape(conn)+"/issues", q)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Issues []cliIssue `json:"issues"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, i := range out.Issues {
		who := ""
		if i.Assignee != nil {
			who = "\t@" + i.Assignee.String()
		}
		c.out("%s\t[%s]\t%s%s\t%s\n", i.Key, i.Status.Name, i.Title, who, i.URL)
	}
	if len(out.Issues) == 0 {
		c.out("no issues\n")
	}
	return nil
}

type cliMR struct {
	IID                 int    `json:"iid"`
	Title               string `json:"title"`
	URL                 string `json:"url"`
	State               string `json:"state"`
	Draft               bool   `json:"draft"`
	Project             string `json:"project"`
	SourceBranch        string `json:"sourceBranch"`
	DetailedMergeStatus string `json:"detailedMergeStatus"`
	Pipeline            *struct {
		Status string `json:"status"`
	} `json:"pipeline"`
}

// mrs: harnessly mrs [--conn ID] [--state opened|merged|closed|all] [--limit N] TEXT… — merge requests of a code host.
func (c *cli) mrs(args []string) error {
	state, rest := "", make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--state":
			if i+1 >= len(args) {
				return usageErr("--state needs a value")
			}
			i++
			state = args[i]
		case strings.HasPrefix(a, "--state="):
			state = strings.TrimPrefix(a, "--state=")
		default:
			rest = append(rest, a)
		}
	}
	if state != "" && state != "opened" && state != "merged" && state != "closed" && state != "all" {
		return usageErr("--state is opened, merged, closed or all")
	}
	conn, q, err := searchFlags(rest, false)
	if err != nil {
		return err
	}
	if state != "" {
		q.Set("state", state)
	}
	if conn == "" {
		if conn, err = c.primaryOf("code"); err != nil {
			return err
		}
	}
	raw, err := c.connGet("/"+url.PathEscape(conn)+"/mrs", q)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		MRs []cliMR `json:"mrs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, m := range out.MRs {
		st := m.State
		if m.Draft {
			st += ",draft"
		}
		ci := "-"
		if m.Pipeline != nil && m.Pipeline.Status != "" {
			ci = m.Pipeline.Status
		}
		c.out("%s!%d\t[%s]\tci:%s\t%s\t%s\n", m.Project, m.IID, st, ci, m.Title, m.URL)
	}
	if len(out.MRs) == 0 {
		c.out("no merge requests\n")
	}
	return nil
}

func isLink(v string) bool {
	return strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://")
}

// issue: harnessly issue KEY|URL [--conn ID].
func (c *cli) issue(args []string) error {
	conn, q, err := searchFlags(args, false)
	if err != nil {
		return err
	}
	ref := q.Get("q")
	if ref == "" || strings.Contains(ref, " ") {
		return usageErr("issue takes one key or link: harnessly issue ABC-12")
	}
	var raw []byte
	if isLink(ref) {
		if raw, err = c.connGet("/resolve", url.Values{"url": {ref}}); err != nil {
			return c.notConnected(err, ref)
		}
	} else {
		if conn == "" {
			if conn, err = c.primaryOf("issues"); err != nil {
				return err
			}
		}
		if raw, err = c.connGet("/"+url.PathEscape(conn)+"/issue", url.Values{"key": {ref}}); err != nil {
			return err
		}
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Issue *cliIssue `json:"issue"`
		Page  *cliPage  `json:"page"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Page != nil {
		c.printPage(out.Page)
		return nil
	}
	if out.Issue == nil {
		return fail("the link is not an issue")
	}
	c.printIssue(out.Issue)
	return nil
}

// notConnected words a link no connection owns.
func (c *cli) notConnected(err error, link string) error {
	if strings.Contains(err.Error(), "refused: not-found") {
		host := link
		if u, e := url.Parse(link); e == nil {
			host = u.Host
		}
		return fail("no connection owns %s: ask the person to connect it in Settings → Connections", host)
	}
	return err
}

func (c *cli) stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(c.zone()).Format("2006-01-02 15:04")
}

func (c *cli) printIssue(i *cliIssue) {
	c.out("%s  %s\n", i.Key, i.Title)
	line := func(k, v string) {
		if v != "" {
			c.out("%-10s%s\n", k+":", v)
		}
	}
	line("status", i.Status.Name)
	line("type", i.Type)
	line("priority", i.Priority)
	line("project", i.Project)
	line("assignee", i.Assignee.String())
	line("reporter", i.Reporter.String())
	if i.Parent != nil {
		line("parent", strings.TrimSpace(i.Parent.Key+" "+i.Parent.Title))
	}
	line("labels", strings.Join(i.Labels, ", "))
	line("updated", c.stamp(i.Updated))
	line("url", i.URL)
	if d := strings.TrimSpace(i.Description); d != "" {
		c.out("\n%s\n", d)
	}
	for _, cm := range i.Comments {
		c.out("\n— %s, %s\n%s\n", cm.Author.String(), c.stamp(cm.Created), strings.TrimSpace(cm.Text))
	}
}

func (c *cli) printPage(p *cliPage) {
	c.out("%s\n", p.Title)
	if p.Space != "" {
		c.out("space:    %s\n", p.Space)
	}
	if p.Author != nil {
		c.out("author:   %s\n", p.Author.String())
	}
	if s := c.stamp(p.Updated); s != "" {
		c.out("updated:  %s\n", s)
	}
	c.out("id:       %s\nurl:      %s\n", p.ID, p.URL)
	if b := strings.TrimSpace(p.Body); b != "" {
		c.out("\n%s\n", b)
	}
}

// pages: harnessly pages [--conn ID] text….
func (c *cli) pages(args []string) error {
	conn, q, err := searchFlags(args, false)
	if err != nil {
		return err
	}
	if q.Get("q") == "" {
		return usageErr("pages needs text to search for: harnessly pages onboarding")
	}
	if conn == "" {
		if conn, err = c.primaryOf("pages"); err != nil {
			return err
		}
	}
	raw, err := c.connGet("/"+url.PathEscape(conn)+"/pages", q)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Pages []cliPage `json:"pages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, p := range out.Pages {
		space := ""
		if p.Space != "" {
			space = "\t[" + p.Space + "]"
		}
		c.out("%s\t%s%s\t%s\n", p.ID, p.Title, space, p.URL)
	}
	if len(out.Pages) == 0 {
		c.out("no pages\n")
	}
	return nil
}

// page: harnessly page ID|URL [--conn ID].
func (c *cli) page(args []string) error {
	conn, q, err := searchFlags(args, false)
	if err != nil {
		return err
	}
	ref := q.Get("q")
	if ref == "" || strings.Contains(ref, " ") {
		return usageErr("page takes one page id or link: harnessly page 123456")
	}
	var raw []byte
	if isLink(ref) {
		if raw, err = c.connGet("/resolve", url.Values{"url": {ref}}); err != nil {
			return c.notConnected(err, ref)
		}
	} else {
		if conn == "" {
			if conn, err = c.primaryOf("pages"); err != nil {
				return err
			}
		}
		if raw, err = c.connGet("/"+url.PathEscape(conn)+"/page", url.Values{"id": {ref}}); err != nil {
			return err
		}
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Issue *cliIssue `json:"issue"`
		Page  *cliPage  `json:"page"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Issue != nil {
		c.printIssue(out.Issue)
		return nil
	}
	if out.Page == nil {
		return fail("the link is not a page")
	}
	c.printPage(out.Page)
	return nil
}
