// Package gitread answers the read-only git questions about a tab's working folder and a repository on this machine:
// branch, status, the working diff, a commit's diff and the commit log. It runs git with the same arguments, limits
// and timeouts as the runtime it replaces and reads the output the same way, so its answers match that runtime's.
package gitread

import (
	"net/url"
	"os"
	"strconv"

	"harnessly/internal/agent"
)

// Tmux is what the routes need of the runtime's tmux servers.
type Tmux interface {
	HasSession(name string) bool
	PanePID(name string) int
	PanePath(name string) string
}

// Service answers the routes; it keeps the short-lived branch and status answers per folder.
type Service struct {
	tm    Tmux
	cache ttlCache
}

func New(tm Tmux) *Service { return &Service{tm: tm} }

type errBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func logError(what string, err error) {
	agent.Log("error", what+" failed", agent.F{K: "error", V: err.Error()})
}

// sessionCwd is a tab session's working folder: its shell's, else tmux's idea of the pane; "" when there is none.
func (s *Service) sessionCwd(session string) string {
	if pid := s.tm.PanePID(session); pid > 0 {
		if cwd, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil && cwd != "" {
			return cwd
		}
	}
	return s.tm.PanePath(session)
}

func forced(q url.Values) bool { f := q.Get("force"); return f == "true" || f == "1" }

var missingSession = errBody{Error: "missing-param", Message: "tmuxSession parameter required"}

// BranchBody is the branch route's answer.
type BranchBody struct {
	Branch *string `json:"branch"`
}

// StatusBody is the status route's answer.
type StatusBody struct {
	Status *Status `json:"status"`
}

// Branch answers /api/git/branch?tmuxSession=&force=.
func (s *Service) Branch(q url.Values) (int, any) {
	session := q.Get("tmuxSession")
	if session == "" {
		return 400, missingSession
	}
	cwd := s.sessionCwd(session)
	if cwd == "" {
		return 404, errBody{Error: "tmux-session-not-found"}
	}
	b, err := s.branch(cwd, forced(q))
	if err != nil {
		logError("branch query", err)
		return 500, errBody{Error: "git-error"}
	}
	return 200, BranchBody{Branch: b}
}

// Status answers /api/git/status?tmuxSession=&force=.
func (s *Service) Status(q url.Values) (int, any) {
	session := q.Get("tmuxSession")
	if session == "" {
		return 400, missingSession
	}
	cwd := s.sessionCwd(session)
	if cwd == "" {
		return 404, errBody{Error: "tmux-session-not-found"}
	}
	st, err := s.status(cwd, forced(q))
	if err != nil {
		logError("status query", err)
		return 500, errBody{Error: "git-error"}
	}
	return 200, StatusBody{Status: st}
}

// tabFolder is the working folder of the session the query names, or the refusal.
func (s *Service) tabFolder(session string) (string, int, any) {
	if !s.tm.HasSession(session) {
		return "", 404, errBody{Error: "Session not found"}
	}
	cwd := s.sessionCwd(session)
	if cwd == "" {
		return "", 500, errBody{Error: "Failed to get CWD"}
	}
	return cwd, 0, nil
}

// Diff answers /api/layout/diff?session=&hashOnly=&fetch=; with fetch=true the remote is fetched first.
func (s *Service) Diff(q url.Values) (int, any) {
	session := q.Get("session")
	if session == "" {
		return 400, errBody{Error: "session parameter required"}
	}
	cwd, status, refusal := s.tabFolder(session)
	if refusal != nil {
		return status, refusal
	}
	return diff(cwd, q.Get("hashOnly") == "true", q.Get("fetch") == "true")
}

// CommitDiff answers /api/layout/commit-diff?session=&hash=.
func (s *Service) CommitDiff(q url.Values) (int, any) {
	session, hash := q.Get("session"), q.Get("hash")
	if session == "" || hash == "" {
		return 400, errBody{Error: "session and hash parameters required"}
	}
	if !hashRe.MatchString(hash) {
		return 400, errBody{Error: "Invalid hash format"}
	}
	cwd, status, refusal := s.tabFolder(session)
	if refusal != nil {
		return status, refusal
	}
	return commitDiff(cwd, hash)
}

// GitLog answers /api/layout/git-log?session=&limit=&skip=.
func (s *Service) GitLog(q url.Values) (int, any) {
	limit, skip := logLimits(q.Get("limit"), q.Get("skip"))
	session := q.Get("session")
	if session == "" {
		return 400, errBody{Error: "session parameter required"}
	}
	cwd, status, refusal := s.tabFolder(session)
	if refusal != nil {
		return status, refusal
	}
	return commitLog(cwd, limit, skip)
}

// CommitChanges answers GET /api/<changes>?root=&sha=: a commit's diff, checked to belong to the repository at root.
// A parameter given more than once counts as missing.
func CommitChanges(q url.Values) (int, any) {
	one := func(k string) string {
		if v := q[k]; len(v) == 1 {
			return v[0]
		}
		return ""
	}
	if c, ok := commitAt(one("root"), one("sha")); ok {
		return 200, c
	}
	return 404, errBody{Error: "Commit not found"}
}
