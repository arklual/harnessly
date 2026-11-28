package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

// One command installs Connect on a computer: the runtime gives a script with the pairing code inside it.
//
//	curl -fsSL https://<runtime>/connect/install/hx_… | sh          macOS, Linux
//	irm https://<runtime>/connect/install/hx_….ps1 | iex             Windows (PowerShell)
//
// The script downloads the release archive for its system, checks it against the release's checksums.txt, puts the
// app in the person's own folder, pairs it with the code, turns autostart on and starts it.

// DefaultReleases is the GitHub repository whose connect-v* releases the install script downloads.
const DefaultReleases = "arklual/harnessly"

var (
	codeRe    = regexp.MustCompile(`^hx_[a-z2-7]{24}$`)
	safeURLRe = regexp.MustCompile(`^https?://[A-Za-z0-9.\-:\[\]]+(/[A-Za-z0-9._~\-/]*)?$`)
)

// InstallCommands are the one-line installers for a code; empty when the runtime's public address is unknown.
func InstallCommands(base, code string) (unix, windows string) {
	base = strings.TrimRight(base, "/")
	if !safeURLRe.MatchString(base) || !codeRe.MatchString(code) {
		return "", ""
	}
	return "curl -fsSL " + base + "/connect/install/" + code + " | sh",
		"irm " + base + "/connect/install/" + code + ".ps1 | iex"
}

// PublicBase is the runtime's address as the person's computer reaches it: Server.URL when set, else the request's
// own host (behind a proxy, its X-Forwarded-* headers); "" for a loopback request (the CLI on this machine).
func (s *Server) PublicBase(r *http.Request) string {
	if s.URL != "" {
		return strings.TrimRight(s.URL, "/")
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return ""
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil && ip.IsLoopback() {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	base := scheme + "://" + host
	if !safeURLRe.MatchString(base) {
		return ""
	}
	return base
}

// release is the newest connect-v* release: its tag and where its files download from.
func (s *Server) release(ctx context.Context) (tag, dl string, err error) {
	if s.Download != "" {
		return "", strings.TrimRight(s.Download, "/"), nil
	}
	repo := s.Releases
	if repo == "" {
		repo = DefaultReleases
	}
	s.relMu.Lock()
	defer s.relMu.Unlock()
	if s.relTag != "" && time.Since(s.relAt) < 10*time.Minute {
		return s.relTag, "https://github.com/" + repo + "/releases/download/" + s.relTag, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases?per_page=50", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "harnessly-runtime")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var list []struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
		Pre   bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", "", err
	}
	best := ""
	for _, r := range list {
		if r.Draft || r.Pre || !strings.HasPrefix(r.Tag, "connect-v") || !releaseTagRe.MatchString(r.Tag) {
			continue
		}
		if best == "" || tagNewer(r.Tag, best) {
			best = r.Tag
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("в %s ещё нет релиза connect-v*", repo)
	}
	s.relTag, s.relAt = best, time.Now()
	return best, "https://github.com/" + repo + "/releases/download/" + best, nil
}

var releaseTagRe = regexp.MustCompile(`^connect-v[0-9]+(\.[0-9]+)*$`)

func tagNewer(a, b string) bool {
	pa := strings.Split(strings.TrimPrefix(a, "connect-v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "connect-v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			fmt.Sscan(pb[i], &y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// ServeInstall is GET /connect/install/<code>[.ps1]: the install script for a pairing code. A code that is unknown or
// expired gives a script that says so; the code itself is spent only when the computer pairs.
func (s *Server) ServeInstall(w http.ResponseWriter, r *http.Request) {
	s.init()
	name := strings.TrimPrefix(r.URL.Path, "/connect/install/")
	ps := strings.HasSuffix(name, ".ps1")
	code := strings.TrimSuffix(name, ".ps1")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	say := func(msg string) {
		if ps {
			fmt.Fprintf(w, "Write-Host %s -ForegroundColor Red\n", psQuote("harnessly connect: "+msg))
		} else {
			fmt.Fprintf(w, "#!/bin/sh\necho %s >&2\nexit 1\n", shQuote("harnessly connect: "+msg))
		}
	}
	if !codeRe.MatchString(code) {
		say("неверный код: возьмите новый (harnessly connect code)")
		return
	}
	s.mu.Lock()
	exp, ok := s.codes[code]
	s.mu.Unlock()
	if !ok || time.Now().After(exp) {
		say("код неверный или истёк: возьмите новый (harnessly connect code)")
		return
	}
	base := s.PublicBase(r)
	if base == "" {
		say("runtime не знает своего адреса: задайте RUNTIME_CONNECT_URL")
		return
	}
	tag, dl, err := s.release(r.Context())
	if err != nil {
		s.logf("connect: install script: %v", err)
		say("не нашёлся релиз Connect: " + err.Error())
		return
	}
	q := shQuote
	if ps {
		q = psQuote
	}
	v := map[string]string{"URL": q(base), "Code": q(code), "DL": q(dl), "Tag": q(tag)}
	t := installSh
	if ps {
		t = installPs
	}
	_ = t.Execute(w, v)
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

var installSh = template.Must(template.New("sh").Parse(`#!/bin/sh
# Harnessly Connect: download, pair with this code, autostart, start.
set -eu
URL={{.URL}}
CODE={{.Code}}
DL={{.DL}}

say() { printf '%s\n' "$*" >&2; }
die() { say "harnessly connect: $*"; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "эта система не поддерживается: $(uname -s); на Windows: irm $URL/connect/install/$CODE.ps1 | iex" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "этот процессор не поддерживается: $(uname -m)" ;;
esac
command -v curl >/dev/null || die "нужен curl"
command -v bash >/dev/null || die "нужен bash: в нём работают команды агентов"

asset="harnessly-connect_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
say "Скачиваю $asset…"
curl -fsSL "$DL/$asset" -o "$tmp/$asset" || die "не скачался $DL/$asset"
curl -fsSL "$DL/checksums.txt" -o "$tmp/checksums.txt" || die "не скачался checksums.txt"
want=$(awk -v n="$asset" '$2 == n { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$asset нет в checksums.txt"
if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}'); fi
[ "$got" = "$want" ] || die "архив не совпадает с checksums.txt"
mkdir -p "$tmp/x"
tar -xzf "$tmp/$asset" -C "$tmp/x"
[ -f "$tmp/x/connect" ] || die "в архиве нет connect"

dir="$HOME/.local/bin"
bin="$dir/harnessly-connect"
mkdir -p "$dir"
# A Connect already running here stops first: it starts again, paired with the new code.
if [ "$os" = darwin ]; then
  launchctl unload "$HOME/Library/LaunchAgents/tech.harnessly.connect.plist" 2>/dev/null || true
elif systemctl --user is-active --quiet harnessly-connect.service 2>/dev/null; then
  systemctl --user stop harnessly-connect.service || true
fi
pkill -f "$bin" 2>/dev/null || true
chmod 755 "$tmp/x/connect"
mv -f "$tmp/x/connect" "$bin"
[ "$os" = darwin ] && xattr -d com.apple.quarantine "$bin" 2>/dev/null || true

"$bin" pair --url "$URL" "$CODE"
auto=yes
"$bin" autostart on || { auto=no; say "автозапуск не включился: Connect работает до перезагрузки"; }

detach() { if command -v setsid >/dev/null; then setsid "$@" </dev/null >/dev/null 2>&1 & else nohup "$@" </dev/null >/dev/null 2>&1 & fi; }
if [ "$os" = darwin ] && [ $auto = yes ]; then
  launchctl load -w "$HOME/Library/LaunchAgents/tech.harnessly.connect.plist"
elif [ "$os" = darwin ] || [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
  detach "$bin"
elif [ $auto = no ]; then
  detach "$bin" run
else
  systemctl --user start harnessly-connect.service
  if command -v loginctl >/dev/null && [ "$(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null)" != yes ]; then
    say "Чтобы Connect работал и без входа в систему: sudo loginctl enable-linger $(id -un)"
  fi
fi
if [ $auto = yes ]; then say "Готово: Connect запущен ($bin) и стартует при входе в систему."
else say "Готово: Connect запущен ($bin)."; fi
`))

var installPs = template.Must(template.New("ps1").Parse(`# Harnessly Connect: download, pair with this code, autostart, start.
& {
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$URL = {{.URL}}
$CODE = {{.Code}}
$DL = {{.DL}}

function Fail($msg) { Write-Host "harnessly connect: $msg" -ForegroundColor Red; throw $msg }

# The agents' commands run in Git Bash, as Claude Code's own do on Windows.
$bash = @("$env:ProgramFiles\Git\bin\bash.exe", "${env:ProgramFiles(x86)}\Git\bin\bash.exe", "$env:LOCALAPPDATA\Programs\Git\bin\bash.exe") |
  Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $bash -and -not (Get-Command git -ErrorAction SilentlyContinue)) {
  if (Get-Command winget -ErrorAction SilentlyContinue) {
    Write-Host 'Ставлю Git for Windows (в Git Bash работают команды агентов)…'
    winget install --id Git.Git -e --source winget --accept-package-agreements --accept-source-agreements --silent
  } else {
    Write-Host 'Нужен Git for Windows (https://git-scm.com/download/win): без него команды агентов не запустятся.' -ForegroundColor Yellow
  }
}

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "harnessly-connect_windows_$arch.zip"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("harnessly-connect-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "Скачиваю $asset…"
  Invoke-WebRequest -UseBasicParsing "$DL/$asset" -OutFile "$tmp\$asset"
  Invoke-WebRequest -UseBasicParsing "$DL/checksums.txt" -OutFile "$tmp\checksums.txt"
  $want = (Get-Content "$tmp\checksums.txt" | ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $asset) { $f[0] } }) | Select-Object -First 1
  if (-not $want) { Fail "$asset нет в checksums.txt" }
  $got = (Get-FileHash -Algorithm SHA256 "$tmp\$asset").Hash.ToLower()
  if ($got -ne $want.ToLower()) { Fail 'архив не совпадает с checksums.txt' }
  Expand-Archive -Force "$tmp\$asset" "$tmp\x"
  if (-not (Test-Path "$tmp\x\connect.exe")) { Fail 'в архиве нет connect.exe' }

  $dir = Join-Path $env:LOCALAPPDATA 'Programs\HarnesslyConnect'
  $exe = Join-Path $dir 'harnessly-connect.exe'
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  # A Connect already running here stops first: it starts again, paired with the new code.
  Get-Process -Name 'harnessly-connect' -ErrorAction SilentlyContinue | Stop-Process -Force
  Start-Sleep -Milliseconds 500
  Copy-Item -Force "$tmp\x\connect.exe" $exe
  Unblock-File $exe -ErrorAction SilentlyContinue

  # connect.exe is a window app: its answers come through files.
  function Run-Connect($argv) {
    $p = Start-Process -FilePath $exe -ArgumentList $argv -Wait -PassThru -NoNewWindow -RedirectStandardOutput "$tmp\out.txt" -RedirectStandardError "$tmp\err.txt"
    Get-Content "$tmp\out.txt" -ErrorAction SilentlyContinue | Write-Host
    $e = Get-Content "$tmp\err.txt" -ErrorAction SilentlyContinue
    if ($p.ExitCode -ne 0) { Fail (($e -join ' ') + " (код $($p.ExitCode))") }
  }
  Run-Connect @('pair', '--url', $URL, $CODE)
  Run-Connect @('autostart', 'on')
  Start-Process -FilePath $exe
  Write-Host "Готово: Connect запущен (значок в трее) и стартует при входе в Windows."
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
}
`))

// relCache is the release the install scripts point at, looked up at most every 10 minutes.
type relCache struct {
	relMu  sync.Mutex
	relTag string
	relAt  time.Time
}
