package webhook

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// item is one thing a request says happened: its kind (alert.firing, ci.failed, pr.opened…) and the short facts a rule
// filters on. Title and URL stay on the machine and in the inbox; the hub gets neither.
type item struct {
	Kind   string
	Title  string
	URL    string
	Actor  string
	Thread string
	// Sub tells several items of one request apart (the alerts of a group).
	Sub   string
	Facts map[string]string
}

var fieldPathRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+){0,7}$`)

func fieldPathOK(p string) bool { return fieldPathRe.MatchString(p) }

// at reads a dotted path of a JSON value: "pull_request.head.ref", "alerts.0.labels.alertname".
func at(v any, path string) any {
	for _, part := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// s is a dotted path as a string ("" when it is not a string or a number).
func s(v any, path string) string {
	switch x := at(v, path).(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// token is a value fit for a fact or a kind: a short identifier, never free text.
var tokenRe = regexp.MustCompile(`[^a-z0-9_.-]+`)

func token(v string) string {
	v = tokenRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(v)), "_")
	if len(v) > 60 {
		v = v[:60]
	}
	return strings.Trim(v, "_")
}

// facts keeps the non-empty ones; values are identifiers (a repo, a branch, a severity), cut short by the envelope.
func facts(kv ...string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		if v := strings.TrimSpace(kv[i+1]); v != "" {
			out[kv[i]] = v
		}
	}
	return out
}

// parse turns a verified body into the things it says happened, by the connection's preset.
func parse(in connectors.Instance, h http.Header, body any) []item {
	switch preset(in) {
	case PresetGitHub:
		return github(h.Get("X-GitHub-Event"), body)
	case PresetGitLab:
		return gitlab(body)
	case PresetGrafana, PresetAlertmanager:
		return alerts(body)
	case PresetSentry:
		return sentry(h.Get("Sentry-Hook-Resource"), body)
	}
	return anyJSON(in, body)
}

func ciKind(status string) string {
	switch token(status) {
	case "failure", "failed", "timed_out", "startup_failure":
		return "ci.failed"
	case "success", "passed":
		return "ci.passed"
	case "cancelled", "canceled":
		return "ci.canceled"
	case "":
		return "ci.changed"
	}
	return "ci." + token(status)
}

func github(event string, b any) []item {
	repo := s(b, "repository.full_name")
	action := token(s(b, "action"))
	actor := s(b, "sender.login")
	switch token(event) {
	case "ping", "":
		return nil
	case "pull_request":
		kind := "pr." + action
		if action == "closed" && at(b, "pull_request.merged") == true {
			kind = "pr.merged"
		}
		num := s(b, "pull_request.number")
		return []item{{Kind: kind, Title: s(b, "pull_request.title"), URL: s(b, "pull_request.html_url"), Actor: actor,
			Thread: repo + "#" + num, Facts: facts("repo", repo, "number", num, "branch", s(b, "pull_request.head.ref"),
				"base", s(b, "pull_request.base.ref"), "assignee", s(b, "assignee.login"), "reviewer", s(b, "requested_reviewer.login"))}}
	case "pull_request_review":
		num := s(b, "pull_request.number")
		return []item{{Kind: "review." + token(s(b, "review.state")), Title: s(b, "pull_request.title"), URL: s(b, "review.html_url"),
			Actor: actor, Thread: repo + "#" + num, Facts: facts("repo", repo, "number", num)}}
	case "issues":
		num := s(b, "issue.number")
		return []item{{Kind: "issue." + action, Title: s(b, "issue.title"), URL: s(b, "issue.html_url"), Actor: actor,
			Thread: repo + "#" + num, Facts: facts("repo", repo, "number", num, "assignee", s(b, "assignee.login"))}}
	case "issue_comment":
		num := s(b, "issue.number")
		return []item{{Kind: "comment." + action, Title: s(b, "issue.title"), URL: s(b, "comment.html_url"), Actor: actor,
			Thread: repo + "#" + num, Facts: facts("repo", repo, "number", num)}}
	case "workflow_run":
		if action != "completed" {
			return nil
		}
		branch := s(b, "workflow_run.head_branch")
		return []item{{Kind: ciKind(s(b, "workflow_run.conclusion")), Title: s(b, "workflow_run.name"), URL: s(b, "workflow_run.html_url"),
			Actor: actor, Thread: repo + "@" + branch, Facts: facts("repo", repo, "branch", branch, "workflow", token(s(b, "workflow_run.name")))}}
	case "check_suite":
		if action != "completed" {
			return nil
		}
		branch := s(b, "check_suite.head_branch")
		return []item{{Kind: ciKind(s(b, "check_suite.conclusion")), Actor: actor, Thread: repo + "@" + branch,
			Facts: facts("repo", repo, "branch", branch)}}
	case "push":
		branch := strings.TrimPrefix(s(b, "ref"), "refs/heads/")
		n := 0
		if c, ok := at(b, "commits").([]any); ok {
			n = len(c)
		}
		return []item{{Kind: "push", URL: s(b, "compare"), Actor: actor, Thread: repo + "@" + branch,
			Facts: facts("repo", repo, "branch", branch, "commits", strconv.Itoa(n))}}
	case "release":
		return []item{{Kind: "release." + action, Title: s(b, "release.name"), URL: s(b, "release.html_url"), Actor: actor,
			Facts: facts("repo", repo, "tag", s(b, "release.tag_name"))}}
	}
	kind := "github." + token(event)
	if action != "" {
		kind += "." + action
	}
	return []item{{Kind: kind, Actor: actor, Facts: facts("repo", repo)}}
}

var gitlabAction = map[string]string{"open": "opened", "close": "closed", "reopen": "reopened", "merge": "merged",
	"update": "updated", "approved": "approved", "unapproved": "unapproved"}

func gitlabVerb(a string) string {
	if v, ok := gitlabAction[token(a)]; ok {
		return v
	}
	if a == "" {
		return "changed"
	}
	return token(a)
}

func gitlab(b any) []item {
	project := s(b, "project.path_with_namespace")
	actor := s(b, "user.username")
	switch token(s(b, "object_kind")) {
	case "merge_request":
		iid := s(b, "object_attributes.iid")
		return []item{{Kind: "pr." + gitlabVerb(s(b, "object_attributes.action")), Title: s(b, "object_attributes.title"),
			URL: s(b, "object_attributes.url"), Actor: actor, Thread: project + "!" + iid,
			Facts: facts("repo", project, "number", iid, "branch", s(b, "object_attributes.source_branch"), "base", s(b, "object_attributes.target_branch"))}}
	case "issue", "work_item":
		iid := s(b, "object_attributes.iid")
		kind := "issue." + gitlabVerb(s(b, "object_attributes.action"))
		if at(b, "changes.assignees") != nil {
			kind = "issue.assigned"
		}
		return []item{{Kind: kind, Title: s(b, "object_attributes.title"), URL: s(b, "object_attributes.url"), Actor: actor,
			Thread: project + "#" + iid, Facts: facts("repo", project, "number", iid, "assignee", s(b, "assignees.0.username"))}}
	case "pipeline":
		status := s(b, "object_attributes.status")
		switch token(status) {
		case "failed", "success", "canceled":
		default:
			return nil
		}
		branch := s(b, "object_attributes.ref")
		id := s(b, "object_attributes.id")
		url := ""
		if web := s(b, "project.web_url"); web != "" && id != "" {
			url = web + "/-/pipelines/" + id
		}
		return []item{{Kind: ciKind(status), Title: project + " " + branch, URL: url, Actor: actor, Thread: project + "@" + branch,
			Facts: facts("repo", project, "branch", branch, "pipeline", id)}}
	case "build":
		if token(s(b, "build_status")) != "failed" {
			return nil
		}
		branch := s(b, "ref")
		return []item{{Kind: "ci.job_failed", Title: s(b, "build_name"), Actor: actor, Thread: s(b, "project_name") + "@" + branch,
			Facts: facts("branch", branch, "stage", token(s(b, "build_stage")))}}
	case "note":
		return []item{{Kind: "comment.created", URL: s(b, "object_attributes.url"), Actor: actor,
			Facts: facts("repo", project, "on", token(s(b, "object_attributes.noteable_type")))}}
	case "push", "tag_push":
		branch := strings.TrimPrefix(s(b, "ref"), "refs/heads/")
		return []item{{Kind: "push", Actor: s(b, "user_username"), Thread: project + "@" + branch,
			Facts: facts("repo", project, "branch", branch, "commits", s(b, "total_commits_count"))}}
	}
	return []item{{Kind: "gitlab." + token(s(b, "object_kind")), Actor: actor, Facts: facts("repo", project)}}
}

// alerts reads the Alertmanager webhook format, which Grafana's webhook contact point sends too: one item per alert.
func alerts(b any) []item {
	list, _ := at(b, "alerts").([]any)
	var out []item
	for i, a := range list {
		status := token(s(a, "status"))
		if status == "" {
			status = token(s(b, "status"))
		}
		name := s(a, "labels.alertname")
		fp := s(a, "fingerprint")
		url := s(a, "panelURL")
		if url == "" {
			url = s(a, "dashboardURL")
		}
		if url == "" {
			url = s(a, "generatorURL")
		}
		thread := fp
		if thread == "" {
			thread = name
		}
		out = append(out, item{Kind: "alert." + status, Title: name, URL: url, Thread: "alert:" + thread,
			Sub:   strconv.Itoa(i) + ":" + fp + ":" + status + ":" + s(a, "startsAt"),
			Facts: facts("alert", name, "status", status, "severity", token(s(a, "labels.severity")), "fingerprint", fp)})
		if len(out) == 50 {
			break
		}
	}
	return out
}

func sentry(resource string, b any) []item {
	action := token(s(b, "action"))
	actor := s(b, "actor.name")
	switch token(resource) {
	case "installation", "":
		return nil
	case "issue":
		id := s(b, "data.issue.id")
		url := s(b, "data.issue.web_url")
		if url == "" {
			url = s(b, "data.issue.permalink")
		}
		return []item{{Kind: "issue." + action, Title: s(b, "data.issue.title"), URL: url, Actor: actor, Thread: "sentry:" + id,
			Facts: facts("project", s(b, "data.issue.project.slug"), "level", token(s(b, "data.issue.level")), "issue", s(b, "data.issue.shortId"))}}
	case "event_alert":
		return []item{{Kind: "alert.firing", Title: s(b, "data.event.title"), URL: s(b, "data.event.web_url"), Actor: actor,
			Thread: "sentry:" + s(b, "data.event.issue_id"),
			Facts:  facts("rule", token(s(b, "data.triggered_rule")), "level", token(s(b, "data.event.level")), "severity", token(s(b, "data.event.level")))}}
	case "metric_alert":
		kind := "alert.firing"
		if action == "resolved" {
			kind = "alert.resolved"
		}
		return []item{{Kind: kind, Title: s(b, "data.description_title"), URL: s(b, "data.web_url"), Actor: actor,
			Thread: "sentry-metric:" + s(b, "data.metric_alert.id"), Facts: facts("severity", action, "status", action)}}
	case "error":
		return []item{{Kind: "error.created", Title: s(b, "data.error.title"), URL: s(b, "data.error.web_url"), Actor: actor,
			Facts: facts("level", token(s(b, "data.error.level")))}}
	}
	return []item{{Kind: "sentry." + token(resource), Actor: actor}}
}

func anyJSON(in connectors.Instance, b any) []item {
	it := item{Kind: "json"}
	if f := in.Config["titleField"]; f != "" {
		it.Title = s(b, f)
	}
	if f := in.Config["urlField"]; f != "" {
		it.URL = s(b, f)
	}
	for _, k := range []string{"type", "event", "kind"} {
		if v := token(s(b, k)); v != "" && !slices.Contains([]string{"true", "false"}, v) {
			it.Facts = facts("type", v)
			break
		}
	}
	return []item{it}
}
