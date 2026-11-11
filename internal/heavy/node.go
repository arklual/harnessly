package heavy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A node check runs on the remote machine too: tsc --noEmit, vitest run, eslint without --fix, npx and npm/pnpm exec
// of those, and a package script made only of them (pnpm test, npm run lint, pnpm typecheck). So does a build: tsc
// that emits (tsc -b), vite build, next build and a script made of them and checks (pnpm build); what it wrote in the
// tree is copied back here after it. node_modules is not copied: the remote machine installs it from the tree's
// lockfile (pnpm-lock.yaml or package-lock.json) once per lockfile and keeps it between calls. A call that changes
// sources (eslint --fix, vitest -u, prettier --write), writes coverage or needs this machine (playwright, vitest
// --browser, a watch) stays here.

// lockfiles are the lockfiles the remote machine installs node_modules from.
var lockfiles = []string{"pnpm-lock.yaml", "package-lock.json"}

// nodeFits tells whether a heavy node call may run on the remote machine, the folder of the lockfile its
// node_modules is installed from there, and whether what it builds is copied back.
func nodeFits(tool string, args []string, cwd, top string) (deps string, back, ok bool) {
	switch {
	case nodeCheck(tool, args, cwd, top, 0, false):
	case nodeCheck(tool, args, cwd, top, 0, true):
		back = true
	default:
		return "", false, false
	}
	deps = lockDir(cwd, top)
	return deps, back, deps != ""
}

// nodeCheck is a call that only reads the tree, its result being its output and exit code; with build, also a call
// that writes its build output into the tree.
func nodeCheck(tool string, args []string, cwd, top string, depth int, build bool) bool {
	if depth > 3 {
		return false
	}
	switch tool {
	case "vitest":
		return vitestFits(args)
	case "tsc", "vue-tsc":
		return tscFits(args, build)
	case "vite", "next":
		return build && bundlerBuilds(args)
	case "eslint":
		return eslintFits(args)
	case "prettier":
		return prettierChecks(args)
	case "npx":
		name, rest, ok := npxTarget(args)
		return ok && nodeCheck(name, rest, cwd, top, depth+1, build)
	case "npm", "pnpm":
		return npmFits(tool, args, cwd, top, depth, build)
	}
	return false
}

func vitestFits(args []string) bool {
	sub := ""
	for _, a := range args {
		name, ok := flagName(a)
		if !ok {
			if sub == "" {
				sub = a
			}
			continue
		}
		switch {
		case name == "u" || name == "update" || name == "ui" || name == "open" || name == "watch" || name == "w" ||
			name == "standalone" || name == "outputFile" || strings.HasPrefix(name, "outputFile.") ||
			name == "coverage" || strings.HasPrefix(name, "coverage.") ||
			name == "browser" || strings.HasPrefix(name, "browser.") || name == "api" || strings.HasPrefix(name, "api."):
			return false
		case name == "reporter" && (strings.HasSuffix(a, "=html") || strings.HasSuffix(a, "=blob")):
			return false
		}
	}
	// A path or a filter comes after `run`; a bare `vitest <filter>` runs once without a terminal, as here.
	switch sub {
	case "init", "bench", "dev", "watch", "serve":
		return false
	}
	return true
}

func tscFits(args []string, build bool) bool {
	noEmit := false
	for _, a := range args {
		name, ok := flagName(a)
		if !ok {
			continue
		}
		switch name {
		case "noEmit":
			noEmit = !strings.HasSuffix(a, "=false")
		case "w", "watch", "init", "generateTrace", "listFilesOnly", "showConfig":
			return false
		case "build", "b":
			if !build {
				return false
			}
		}
	}
	return noEmit || build
}

// bundlerBuilds is `vite build` or `next build` once, without a watch.
func bundlerBuilds(args []string) bool {
	sub := ""
	for _, a := range args {
		name, ok := flagName(a)
		if !ok {
			if sub == "" {
				sub = a
			}
			continue
		}
		if name == "watch" || name == "w" || name == "debug" || name == "profile" {
			return false
		}
	}
	return sub == "build"
}

func eslintFits(args []string) bool {
	for _, a := range args {
		if name, ok := flagName(a); ok {
			switch name {
			case "fix", "fix-type", "o", "output-file", "init", "cache-location":
				return false
			}
		}
	}
	return true
}

func prettierChecks(args []string) bool {
	check := false
	for _, a := range args {
		if name, ok := flagName(a); ok {
			switch name {
			case "write", "w":
				return false
			case "check", "c", "list-different", "l":
				check = true
			}
		}
	}
	return check
}

// npxTarget is the tool npx runs and its arguments; false for -p and -c, whose tool is not the first word.
func npxTarget(args []string) (string, []string, bool) {
	for i, a := range args {
		switch a {
		case "-p", "--package", "-c", "--call":
			return "", nil, false
		}
		if strings.HasPrefix(a, "--package=") || strings.HasPrefix(a, "--call=") {
			return "", nil, false
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		name := a
		if at := strings.LastIndex(name, "@"); at > 0 {
			name = name[:at]
		}
		return name, args[i+1:], true
	}
	return "", nil, false
}

// npmFits is npm or pnpm running a script or a tool in this folder: another folder, a workspace filter or a
// recursive run stays here.
func npmFits(tool string, args []string, cwd, top string, depth int, build bool) bool {
	var words []string
	for i, a := range args {
		if a == "--" {
			words = args[i+1:]
			break
		}
		if strings.HasPrefix(a, "-") {
			name, _ := flagName(a)
			switch name {
			case "C", "dir", "prefix", "w", "workspace", "workspaces", "filter", "F", "r", "recursive",
				"filter-prod", "include-workspace-root":
				return false
			}
			continue
		}
		words = args[i:]
		break
	}
	if len(words) == 0 {
		return false
	}
	rest := words[1:]
	switch words[0] {
	case "test", "t", "tst":
		return scriptFits("test", rest, cwd, top, depth, build)
	case "run", "run-script", "rum", "urn":
		for i, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return scriptFits(a, rest[i+1:], cwd, top, depth, build)
			}
		}
		return false
	case "exec", "x", "dlx":
		if words[0] == "dlx" {
			return false
		}
		return nodeCheck("npx", rest, cwd, top, depth+1, build)
	}
	if tool != "pnpm" {
		return false
	}
	if _, scripts := packageScripts(cwd, top); scripts[words[0]] != "" {
		return scriptFits(words[0], rest, cwd, top, depth, build)
	}
	return nodeCheck(words[0], rest, cwd, top, depth+1, build)
}

// scriptFits tells whether a package script, with the arguments passed to it, only checks the tree. npm also runs
// its pre and post scripts.
func scriptFits(name string, extra []string, cwd, top string, depth int, build bool) bool {
	dir, scripts := packageScripts(cwd, top)
	text := scripts[name]
	if text == "" {
		return false
	}
	if len(extra) > 0 && extra[0] == "--" {
		extra = extra[1:]
	}
	if !commandFits(text+" "+strings.Join(extra, " "), dir, top, depth+1, build) {
		return false
	}
	for _, hook := range []string{"pre" + name, "post" + name} {
		if t := scripts[hook]; t != "" && !commandFits(t, dir, top, depth+1, build) {
			return false
		}
	}
	return true
}

var (
	shellSpecial = regexp.MustCompile("[;|<>`\\n]|\\$\\(|(^|[^&])&($|[^&])")
	assignment   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// commandFits tells whether a script's command line is checks joined by &&: no pipes, redirects, subshells or ;.
func commandFits(line, dir, top string, depth int, build bool) bool {
	if shellSpecial.MatchString(line) {
		return false
	}
	for _, part := range strings.Split(line, "&&") {
		words := strings.Fields(part)
		for len(words) > 0 && (assignment.MatchString(words[0]) || words[0] == "cross-env") {
			words = words[1:]
		}
		if len(words) == 0 || !nodeCheck(words[0], words[1:], dir, top, depth, build) {
			return false
		}
	}
	return true
}

// packageScripts are the scripts of the nearest package.json from dir up to the tree's top, and its folder.
func packageScripts(dir, top string) (string, map[string]string) {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if b, err := os.ReadFile(filepath.Join(d, "package.json")); err == nil {
			var p struct {
				Scripts map[string]string `json:"scripts"`
			}
			_ = json.Unmarshal(b, &p)
			return d, p.Scripts
		}
		if d == top || d == filepath.Dir(d) || !strings.HasPrefix(d, top) {
			return "", nil
		}
	}
}

// lockDir is the nearest folder from dir up to the tree's top with a lockfile the remote machine installs from.
func lockDir(dir, top string) string {
	for d := filepath.Clean(dir); strings.HasPrefix(d, top); d = filepath.Dir(d) {
		for _, l := range lockfiles {
			if _, err := os.Stat(filepath.Join(d, l)); err == nil {
				return d
			}
		}
		if d == top || d == filepath.Dir(d) {
			break
		}
	}
	return ""
}
