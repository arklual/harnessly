package envsetup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)

// --- access ---

func (ex *execution) access(i int) {
	it, src := ex.items[i], ex.srcs[i]
	cli, host := "glab", src.Host
	switch src.Service {
	case "gitlab":
		if host == "" {
			host = "gitlab.com"
		}
	case "github":
		cli = "gh"
		if host == "" {
			host = "github.com"
		}
	default:
		ex.fail(i, "служба должна быть gitlab или github")
		return
	}
	if !hostRe.MatchString(host) {
		ex.fail(i, "адрес сервера не похож на адрес: "+host)
		return
	}
	secret := src.Secret
	if secret == "" {
		secret = hubproto.AccessSecret(src.Service)
	}
	if !hubproto.SafeNameRe.MatchString(secret) {
		ex.fail(i, "секрет назван не так: "+secret)
		return
	}
	noCLI := fmt.Sprintf("нет %s: поставьте его (это может сделать скрипт списка)", cli)
	status := func() (loggedIn bool, ok bool) {
		exit, err := ex.runLogged(i, Cmd{Name: cli, Args: []string{"auth", "status", "--hostname", host}, Env: ex.baseEnv(), Timeout: loginTimeout},
			cli+" auth status --hostname "+host)
		switch {
		case errors.Is(err, exec.ErrNotFound):
			ex.fail(i, noCLI)
			return false, false
		case err != nil:
			ex.fail(i, cli+": "+err.Error())
			return false, false
		}
		return exit == 0, true
	}
	in, ok := status()
	if !ok {
		return
	}
	if !ex.modifies {
		if in {
			ex.done(i, hubproto.EnvOK, "вход в "+host+" выполнен")
		} else {
			ex.pend(i, "нет входа в "+host)
		}
		return
	}
	if !in {
		if !ex.Secrets().Has(secret) {
			ex.needSecret(secret, "Окружение команды: "+it.Name)
			ex.pend(i, "положите токен "+secret+" в сейф")
			return
		}
		if err := ex.reveal([]string{secret}); err != nil {
			ex.fail(i, "сейф не отдал "+secret+": "+err.Error())
			return
		}
		args := []string{"auth", "login", "--hostname", host, "--stdin"}
		if cli == "gh" {
			args = []string{"auth", "login", "--hostname", host, "--with-token"}
		}
		exit, err := ex.runLogged(i, Cmd{Name: cli, Args: args, Env: ex.baseEnv(), Stdin: ex.values[secret] + "\n", Timeout: loginTimeout},
			cli+" "+strings.Join(args, " ")+" < токен из сейфа")
		if err != nil || exit != 0 {
			ex.fail(i, fmt.Sprintf("вход в %s не удался%s", host, exitNote(exit, err)))
			return
		}
	}
	if err := ex.gitHelper(i, cli, host); err != nil {
		ex.fail(i, err.Error())
		return
	}
	if in {
		ex.done(i, hubproto.EnvOK, "вход в "+host+" уже выполнен")
	} else {
		ex.done(i, hubproto.EnvOK, "вход в "+host+" выполнен")
	}
}

// Secrets is the run's safe.
func (ex *execution) Secrets() Secrets { return ex.r.Secrets }

func exitNote(exit int, err error) string {
	switch {
	case errors.Is(err, ErrTimeout):
		return " (не закончилось вовремя)"
	case err != nil:
		return " (" + err.Error() + ")"
	}
	return fmt.Sprintf(" (код %d)", exit)
}

// gitHelper makes `git clone` over https use the CLI's login. gh knows how; for glab the credential helper is
// `glab auth git-credential` when this glab has it, else a one-line helper reading the token from glab's config.
func (ex *execution) gitHelper(i int, cli, host string) error {
	if cli == "gh" {
		exit, err := ex.runLogged(i, Cmd{Name: "gh", Args: []string{"auth", "setup-git", "--hostname", host}, Env: ex.baseEnv(), Timeout: loginTimeout},
			"gh auth setup-git --hostname "+host)
		if err != nil || exit != 0 {
			return fmt.Errorf("gh auth setup-git не удался%s", exitNote(exit, err))
		}
		return nil
	}
	helper := "!glab auth git-credential"
	if _, exit, err := ex.quiet(Cmd{Name: "glab", Args: []string{"auth", "git-credential", "--help"}, Env: ex.baseEnv(), Timeout: loginTimeout}); err != nil || exit != 0 {
		helper = `!f() { test "$1" = get && echo username=oauth2 && echo "password=$(glab config get token --host ` + host + `)"; }; f`
	}
	key := "credential.https://" + host + ".helper"
	exit, err := ex.runLogged(i, Cmd{Name: "git", Args: []string{"config", "--global", key, helper}, Env: ex.baseEnv(), Timeout: loginTimeout},
		"git config --global "+key+" '"+helper+"'")
	if err != nil || exit != 0 {
		return fmt.Errorf("git config для входа не удался%s", exitNote(exit, err))
	}
	return nil
}

// --- repositories ---

// repoName is the last part of a repository's address without .git, or the item's name as a plain word.
func repoName(addr, fallback string) string {
	a := strings.TrimRight(strings.TrimSpace(addr), "/")
	a = strings.TrimSuffix(a, ".git")
	if k := strings.LastIndexAny(a, "/:"); k >= 0 {
		a = a[k+1:]
	}
	if plainName.MatchString(a) && a != "." && a != ".." {
		return a
	}
	var b strings.Builder
	for _, r := range strings.ToLower(fallback) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	if n := strings.Trim(b.String(), "-"); n != "" {
		return n
	}
	return "repo"
}

// repoDir is the folder a repository goes to: `~/…`, or relative to the home, or the default under ~/code. It stays in
// the home.
func (ex *execution) repoDir(i int) (string, error) {
	it, src := ex.items[i], ex.srcs[i]
	home := ex.home
	d := strings.TrimSpace(src.Dir)
	switch {
	case d == "":
		d = filepath.Join(home, "code", repoName(src.URL, it.Name))
	case d == "~":
		d = home
	case strings.HasPrefix(d, "~/"):
		d = filepath.Join(home, d[2:])
	case !filepath.IsAbs(d):
		d = filepath.Join(home, d)
	}
	d = filepath.Clean(d)
	if rel, err := filepath.Rel(home, d); err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return "", fmt.Errorf("папка %s вне домашней папки", d)
	}
	return d, nil
}

// normRemote makes the addresses of one repository equal: no scheme, no user, no .git, one case, ssh's `host:path` as
// `host/path`.
func normRemote(u string) string {
	u = strings.TrimSpace(u)
	scheme := strings.Contains(u, "://")
	if scheme {
		u = u[strings.Index(u, "://")+3:]
	}
	if at := strings.Index(u, "@"); at >= 0 && (!strings.Contains(u[:at], "/")) {
		u = u[at+1:]
	}
	if !scheme {
		if c := strings.Index(u, ":"); c >= 0 && !strings.Contains(u[:c], "/") {
			u = u[:c] + "/" + u[c+1:]
		}
	}
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	return strings.ToLower(u)
}

func (ex *execution) repo(i int) {
	it, src := ex.items[i], ex.srcs[i]
	if src.URL == "" {
		ex.fail(i, "у репозитория нет адреса")
		return
	}
	if err := gitURLOK(src.URL); err != nil {
		ex.fail(i, err.Error())
		return
	}
	dir, err := ex.repoDir(i)
	if err != nil {
		ex.fail(i, err.Error())
		return
	}
	fi, err := os.Stat(ex.hostPath(dir))
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !ex.modifies {
			ex.pend(i, "ещё не склонирован: "+dir)
			return
		}
		if msg := ex.lowDisk(ex.r.Home); msg != "" {
			ex.fail(i, msg)
			return
		}
		args := []string{"clone"}
		if src.Branch != "" {
			args = append(args, "--branch", src.Branch)
		}
		args = append(args, "--", src.URL, dir)
		exit, err := ex.runLogged(i, Cmd{Name: "git", Args: args, Env: ex.baseEnv(), Timeout: gitTimeout}, "git "+strings.Join(args, " "))
		if err != nil || exit != 0 {
			ex.fail(i, "git clone не удался"+exitNote(exit, err))
			return
		}
		ex.repoDirs[it.ItemID] = dir
		ex.done(i, hubproto.EnvOK, "склонирован в "+dir)
	case err != nil:
		ex.fail(i, "папка "+dir+" недоступна: "+err.Error())
	case !fi.IsDir():
		ex.fail(i, dir+" — файл, а не папка")
	default:
		// The folder may be another account's (a shared checkout linked into the home): git then refuses to read it as
		// «dubious ownership». Only its address is read here, so the folder is let in for this one command.
		out, exit, err := ex.quiet(Cmd{Name: "git", Args: []string{"-c", "safe.directory=" + dir, "-C", dir, "remote", "get-url", "origin"}, Env: ex.baseEnv(), Timeout: loginTimeout})
		got := strings.TrimSpace(out)
		switch {
		case err != nil || exit != 0 || got == "":
			ex.fail(i, "папка "+dir+" уже есть, но это не клон репозитория")
		case normRemote(got) != normRemote(src.URL):
			ex.fail(i, "в папке "+dir+" другой репозиторий: "+normRemote(got))
		default:
			ex.repoDirs[it.ItemID] = dir
			ex.done(i, hubproto.EnvOK, "уже склонирован в "+dir)
		}
	}
}

// --- scripts ---

// where is the place a script runs from.
type where struct {
	cwd     string
	file    string // the script's file ("" when the item has only a command)
	bind    string // the temporary folder a sandboxed program must still see
	cleanup func()
}

func (ex *execution) script(i int) {
	it, src := ex.items[i], ex.srcs[i]
	if ex.modifies && ex.blocked != "" {
		ex.pend(i, "ждёт, пока починят "+ex.blocked)
		return
	}
	if len(src.Secrets) > 16 {
		ex.scriptFail(i, "в скрипте больше 16 секретов")
		return
	}
	for _, n := range src.Secrets {
		if !hubproto.SafeNameRe.MatchString(n) {
			ex.scriptFail(i, "секрет назван не так: "+n)
			return
		}
	}
	hash := hashOf(it.Source)
	if !ex.modifies {
		ex.scriptLook(i, hash)
		return
	}
	if src.Rerun == "once" && !ex.sandbox && ex.state.Once[it.ItemID] == hash {
		ex.done(i, hubproto.EnvSkipped, "уже выполнен на этой машине")
		return
	}
	w, err := ex.locate(i, false)
	if err != nil {
		ex.scriptFail(i, err.Error())
		return
	}
	defer w.cleanup()

	present := ex.presentSecrets(src.Secrets)
	if src.Rerun == "check" && strings.TrimSpace(src.Check) != "" {
		exit, err := ex.runLogged(i, Cmd{Name: "bash", Args: []string{"-c", src.Check}, Dir: w.cwd, Env: ex.envWith(present), Timeout: checkTimeout, Bind: w.bind},
			"проверка: "+oneLine(src.Check))
		if err == nil && exit == 0 {
			ex.done(i, hubproto.EnvSkipped, "проверка прошла, делать нечего")
			return
		}
		if errors.Is(err, exec.ErrNotFound) {
			ex.scriptFail(i, "нет bash")
			return
		}
	}
	var missing []string
	for _, n := range src.Secrets {
		if !contains(present, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		for _, n := range missing {
			ex.needSecret(n, "Окружение команды: "+it.Name)
		}
		ex.blocked = it.Name
		ex.pend(i, "положите "+strings.Join(missing, ", ")+" в сейф")
		return
	}
	if err := ex.reveal(src.Secrets); err != nil {
		ex.scriptFail(i, "сейф не отдал секреты: "+err.Error())
		return
	}
	if w, err = ex.locate(i, true); err != nil {
		ex.scriptFail(i, err.Error())
		return
	}
	defer w.cleanup()
	run := strings.TrimSpace(src.Run)
	if run == "" {
		if w.file == "" {
			ex.scriptFail(i, "не сказано, что запускать: нужен файл (path) или команда (run)")
			return
		}
		run = `bash "$HARNESSLY_SCRIPT"`
	}
	env := ex.envWith(src.Secrets)
	if w.file != "" {
		env = append(env, "HARNESSLY_SCRIPT="+w.file)
	}
	exit, err := ex.runLogged(i, Cmd{Name: "bash", Args: []string{"-ec", run}, Dir: w.cwd, Env: env, Timeout: scriptTimeout, Bind: w.bind}, oneLine(run))
	ex.run.Steps[i].Exit = &exit
	switch {
	case errors.Is(err, ErrTimeout):
		ex.run.Steps[i].Exit = nil
		ex.scriptFail(i, "не уложился в 15 минут, остановлен")
	case errors.Is(err, context.Canceled):
		ex.run.Steps[i].Exit = nil
		ex.scriptFail(i, "остановлен")
	case err != nil:
		ex.run.Steps[i].Exit = nil
		ex.scriptFail(i, "не запустился: "+err.Error())
	case exit != 0:
		ex.scriptFail(i, fmt.Sprintf("завершился с кодом %d", exit))
	default:
		if src.Rerun == "once" {
			ex.remember(it.ItemID, hash)
		}
		ex.done(i, hubproto.EnvOK, "выполнен")
	}
}

func (ex *execution) scriptFail(i int, msg string) {
	ex.fail(i, msg)
	ex.blocked = ex.items[i].Name
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

func (ex *execution) presentSecrets(names []string) []string {
	var out []string
	for _, n := range names {
		if ex.r.Secrets != nil && ex.r.Secrets.Has(n) {
			out = append(out, n)
		}
	}
	if len(out) > 0 {
		if err := ex.reveal(out); err != nil {
			return nil
		}
	}
	return out
}

// scriptLook is a script's step in a run that only looks.
func (ex *execution) scriptLook(i int, hash string) {
	it, src := ex.items[i], ex.srcs[i]
	switch src.Rerun {
	case "once":
		if ex.state.Once[it.ItemID] == hash {
			ex.done(i, hubproto.EnvOK, "уже выполнен на этой машине")
		} else {
			ex.pend(i, "ещё не запускался")
		}
	case "check":
		if strings.TrimSpace(src.Check) == "" {
			ex.done(i, hubproto.EnvSkipped, "проверка не задана, скрипт запускается при каждом обновлении")
			return
		}
		w, err := ex.locate(i, false)
		if err != nil {
			ex.fail(i, err.Error())
			return
		}
		defer w.cleanup()
		exit, err := ex.runLogged(i, Cmd{Name: "bash", Args: []string{"-c", src.Check}, Dir: w.cwd, Env: ex.envWith(ex.presentSecrets(src.Secrets)), Timeout: checkTimeout, Bind: w.bind},
			"проверка: "+oneLine(src.Check))
		if err == nil && exit == 0 {
			ex.done(i, hubproto.EnvOK, "проверка прошла")
		} else {
			ex.pend(i, "проверка не прошла: нужно обновить")
		}
	default:
		ex.done(i, hubproto.EnvSkipped, "запускается при каждом обновлении")
	}
}

var fileNameRe = plainName

// locate says where the script runs. Without needFile it only finds the folder (a check needs no download); with it,
// the file is written, downloaded or found, and a repository outside the list is cloned.
func (ex *execution) locate(i int, needFile bool) (where, error) {
	src := ex.srcs[i]
	w := where{cleanup: func() {}}
	temp := func() error {
		ex.seq++
		name := fmt.Sprintf("%s-%d-%d", ex.run.RunID, i, ex.seq)
		dir := filepath.Join(ex.r.LinkDir, "environment", "work", name)
		w.bind = dir
		if ex.sandbox {
			dir, w.bind = filepath.Join(ex.home, ".work", name), "" // inside the sandbox's home
		}
		host := ex.hostPath(dir)
		if err := os.MkdirAll(host, 0o700); err != nil {
			return fmt.Errorf("не удалось создать рабочую папку: %w", err)
		}
		w.cwd = dir
		w.cleanup = func() { _ = os.RemoveAll(host) }
		return nil
	}
	fileIn := func(w *where, root string) error {
		p := path.Clean(src.Path)
		if src.Path == "" {
			return nil
		}
		if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
			return fmt.Errorf("путь %s выходит за репозиторий", src.Path)
		}
		w.file = filepath.Join(root, filepath.FromSlash(p))
		if _, err := os.Stat(ex.hostPath(w.file)); err != nil && needFile {
			return fmt.Errorf("в репозитории нет файла %s", src.Path)
		}
		return nil
	}
	from := src.From
	if (from == "" || from == "repo") && src.Repo == "" && src.RepoURL != "" {
		from = "repoUrl" // a repository outside the list, cloned only to run the script
	}
	switch from {
	case "repo":
		dir, ok := ex.repoDirs[src.Repo]
		if !ok {
			return w, fmt.Errorf("репозиторий %s не склонирован", src.Repo)
		}
		w.cwd = dir
		return w, fileIn(&w, dir)
	case "text", "file", "url", "":
		if err := temp(); err != nil {
			return w, err
		}
		if !needFile {
			return w, nil
		}
		name := "script.sh"
		var body []byte
		switch src.From {
		case "url":
			u, err := url.Parse(src.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				w.cleanup()
				return w, errors.New("скрипт по ссылке — только https")
			}
			fetch := ex.r.Fetch
			if fetch == nil {
				fetch = defaultFetch
			}
			raw, err := fetch(ex.ctx, src.URL)
			if err != nil {
				w.cleanup()
				return w, fmt.Errorf("скрипт не скачался: %w", err)
			}
			body = raw
			if b := path.Base(u.Path); fileNameRe.MatchString(b) && b != "." && b != ".." {
				name = b
			}
		case "file":
			body = []byte(src.Body)
			if b := path.Base(src.FileName); fileNameRe.MatchString(b) && b != "." && b != ".." {
				name = b
			}
		default:
			body = []byte(src.Body)
		}
		w.file = filepath.Join(w.cwd, name)
		if err := os.WriteFile(ex.hostPath(w.file), body, 0o700); err != nil {
			w.cleanup()
			return w, fmt.Errorf("файл скрипта не записан: %w", err)
		}
		return w, nil
	}
	if src.RepoURL == "" {
		return w, fmt.Errorf("неизвестный источник скрипта: %s", src.From)
	}
	if err := gitURLOK(src.RepoURL); err != nil {
		return w, err
	}
	return ex.locateCache(i, w, needFile, fileIn, temp)
}

// locateCache finds the clone of a repository outside the list, kept under the link's folder (in a sandbox: in the
// sandbox's home) only to run a script of it.
func (ex *execution) locateCache(i int, w where, needFile bool, fileIn func(*where, string) error, temp func() error) (where, error) {
	src := ex.srcs[i]
	sum := hashOf([]byte(fmt.Sprintf("%q", src.RepoURL+"\n"+src.Branch)))[:16]
	dir := filepath.Join(ex.r.LinkDir, "environment", "cache", sum)
	if ex.sandbox {
		dir = filepath.Join(ex.home, ".cache", "harnessly-env", sum)
	}
	_, statErr := os.Stat(ex.hostPath(dir))
	exists := statErr == nil
	if !needFile {
		if !exists {
			return w, temp()
		}
		w.cwd = dir
		return w, nil
	}
	var args []string
	if exists {
		args = []string{"-C", dir, "pull", "--ff-only"}
	} else {
		if msg := ex.lowDisk(ex.r.Home); msg != "" {
			return w, errors.New(msg)
		}
		args = []string{"clone", "--depth", "1"}
		if src.Branch != "" {
			args = append(args, "--branch", src.Branch)
		}
		args = append(args, "--", src.RepoURL, dir)
	}
	exit, err := ex.runLogged(i, Cmd{Name: "git", Args: args, Env: ex.baseEnv(), Timeout: gitTimeout}, "git "+strings.Join(args, " "))
	if err != nil || exit != 0 {
		return w, fmt.Errorf("репозиторий скрипта не получен%s", exitNote(exit, err))
	}
	w.cwd = dir
	return w, fileIn(&w, dir)
}

// scpLike is git's `user@host:path` form of an ssh address.
var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/][^\s]*$|^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:/[^\s]*$`)

// gitURLOK lets only https and ssh addresses through: git would also take git://, http://, file:// and ext::, which
// reads local files or runs a program.
func gitURLOK(u string) error {
	u = strings.TrimSpace(u)
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "ssh://"):
		return nil
	case strings.Contains(u, "::"), strings.HasPrefix(u, "-"):
	case !strings.Contains(u, "://") && scpLike.MatchString(u):
		return nil
	}
	return errors.New("репозиторий — только по https или ssh")
}
