// Package basepkg is the base package of a machine: the tools without which a part of harnessly does not work. It is
// one list for everything that looks at it — the machine checks, the runtime (the personal part) and the small root
// service that installs the system part (docs/hub/machine.md, «Base package»).
package basepkg

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Manager is a system package manager.
type Manager string

const (
	Apt    Manager = "apt"
	Dnf    Manager = "dnf"
	Yum    Manager = "yum"
	Pacman Manager = "pacman"
	Zypper Manager = "zypper"
	Apk    Manager = "apk"
	Brew   Manager = "brew"
)

// Item is one thing of the base package.
type Item struct {
	ID    string
	Title string
	// Without is what does not work without the item, said to the person.
	Without string
	// Bins: the item is in place when any of these is on PATH.
	Bins []string
	// Pkgs are the packages that put it in, per package manager; a manager without an entry cannot install it.
	Pkgs map[Manager][]string
	// SizeMB is about how much disk the item takes; it is installed only when 2 GiB stay free after it.
	SizeMB int
	// Heavy items wait while the machine is busy.
	Heavy bool
	// Optional items only add a feature: their absence is not a problem of the first screen and does not keep the
	// base package from being done. They are installed all the same where the machine can.
	Optional bool
	// Personal items are the runtime's own (the person's data), not the system's.
	Personal bool
	// Hosts are the addresses the item needs besides the package mirror.
	Hosts []string
}

// same is a package of one name everywhere.
func same(name string, on ...Manager) map[Manager][]string {
	if len(on) == 0 {
		on = []Manager{Apt, Dnf, Yum, Pacman, Zypper, Apk, Brew}
	}
	out := map[Manager][]string{}
	for _, m := range on {
		out[m] = []string{name}
	}
	return out
}

func with(base map[Manager][]string, m Manager, pkgs ...string) map[Manager][]string {
	base[m] = pkgs
	return base
}

// VoiceID is the personal item of the speech model.
const VoiceID = "voice"

// List is the base package, in the order it is installed (what others need first).
var List = []Item{
	{ID: "tmux", Title: "tmux", Without: "не откроются чаты, вкладки и терминал", Bins: []string{"tmux"}, Pkgs: same("tmux"), SizeMB: 5},
	{ID: "git", Title: "git", Without: "не будет репозиториев и истории изменений", Bins: []string{"git"}, Pkgs: same("git"), SizeMB: 40},
	{ID: "bash", Title: "bash", Without: "не запустятся скрипты настройки", Bins: []string{"bash"}, Pkgs: same("bash"), SizeMB: 5},
	{ID: "curl", Title: "curl", Without: "не скачаются инструменты и модели", Bins: []string{"curl"}, Pkgs: same("curl"), SizeMB: 5},
	{ID: "ssh", Title: "ssh", Without: "не будет доступа к репозиториям по ssh", Bins: []string{"ssh"},
		Pkgs: with(with(with(same("openssh", Dnf, Yum, Pacman, Zypper, Apk, Brew), Apt, "openssh-client"), Dnf, "openssh-clients"), Yum, "openssh-clients"), SizeMB: 10},
	{ID: "python3", Title: "Python 3", Without: "часть скриптов настройки упадёт", Bins: []string{"python3"},
		Pkgs: with(with(same("python3"), Pacman, "python"), Brew, "python"), SizeMB: 60},
	{ID: "unzip", Title: "unzip", Without: "не будет импорта и экспорта PPTX", Bins: []string{"unzip"}, Pkgs: same("unzip"), SizeMB: 2},
	{ID: "node", Title: "Node.js и npm", Without: "не поставятся и не запустятся агенты", Bins: []string{"npm"},
		Pkgs: with(with(with(same("nodejs", Apt, Dnf, Yum, Zypper, Apk), Apt, "nodejs", "npm"), Pacman, "nodejs", "npm"), Brew, "node"), SizeMB: 120},
	{ID: "ffmpeg", Title: "ffmpeg", Without: "не будет голосового ввода, записи и видео", Bins: []string{"ffmpeg"}, Pkgs: same("ffmpeg"), SizeMB: 150},
	{ID: "bubblewrap", Title: "bubblewrap", Without: "не запустятся приложения (плагины)", Bins: []string{"bwrap"},
		Pkgs: same("bubblewrap", Apt, Dnf, Yum, Pacman, Zypper, Apk), SizeMB: 1},
	{ID: "jq", Title: "jq", Without: "агенты медленнее разбирают ответы сервисов", Bins: []string{"jq"}, Pkgs: same("jq"), SizeMB: 2},
	{ID: "ripgrep", Title: "ripgrep", Without: "агенты медленнее ищут по коду", Bins: []string{"rg"}, Pkgs: same("ripgrep"), SizeMB: 6},
	{ID: "chrome", Title: "Chromium", Without: "не будет проверки макетов и экспорта PNG, PDF, MP4",
		Bins: []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"},
		Pkgs: same("chromium", Apt, Dnf, Yum, Pacman, Zypper, Apk), SizeMB: 400, Heavy: true},
	{ID: "libreoffice", Title: "LibreOffice", Without: "не будет импорта и экспорта PPTX", Bins: []string{"soffice", "libreoffice"},
		Pkgs:   with(with(same("libreoffice", Dnf, Yum, Pacman, Zypper, Apk), Apt, "libreoffice-impress"), Dnf, "libreoffice-impress"),
		SizeMB: 700, Heavy: true, Optional: true},
	{ID: VoiceID, Title: "Модель голоса", Without: "не будет голосового ввода", SizeMB: 500, Heavy: true, Personal: true,
		Hosts: []string{"huggingface.co", "github.com"}},
}

// ByID finds an item of the list.
func ByID(id string) (Item, bool) {
	for _, it := range List {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// System is the part of the list a package manager installs.
func System() []Item {
	var out []Item
	for _, it := range List {
		if !it.Personal {
			out = append(out, it)
		}
	}
	return out
}

// Hash names this version of the list: a machine installs again when it changes.
func Hash() string {
	h := sha256.New()
	for _, it := range List {
		h.Write([]byte(it.ID + "|" + strings.Join(it.Bins, ",") + "|"))
		ms := make([]string, 0, len(it.Pkgs))
		for m := range it.Pkgs {
			ms = append(ms, string(m))
		}
		sort.Strings(ms)
		for _, m := range ms {
			h.Write([]byte(m + "=" + strings.Join(it.Pkgs[Manager(m)], " ") + ";"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Detect is the package manager of this machine, "" when there is none known.
func Detect(lookPath func(string) (string, error), goos string) Manager {
	if goos == "darwin" {
		if _, err := lookPath("brew"); err == nil {
			return Brew
		}
		return ""
	}
	for _, c := range []struct {
		bin string
		m   Manager
	}{{"apt-get", Apt}, {"dnf", Dnf}, {"yum", Yum}, {"pacman", Pacman}, {"zypper", Zypper}, {"apk", Apk}} {
		if _, err := lookPath(c.bin); err == nil {
			return c.m
		}
	}
	return ""
}

// InstallArgs is the command that installs pkgs with m, without sudo (nil: m cannot).
func InstallArgs(m Manager, pkgs []string) []string {
	if len(pkgs) == 0 {
		return nil
	}
	var head []string
	switch m {
	case Apt:
		head = []string{"apt-get", "install", "-y", "-q", "--no-install-recommends"}
	case Dnf:
		head = []string{"dnf", "install", "-y", "-q"}
	case Yum:
		head = []string{"yum", "install", "-y", "-q"}
	case Pacman:
		head = []string{"pacman", "-S", "--needed", "--noconfirm"}
	case Zypper:
		head = []string{"zypper", "--non-interactive", "install"}
	case Apk:
		head = []string{"apk", "add", "--no-progress"}
	case Brew:
		head = []string{"brew", "install"}
	default:
		return nil
	}
	return append(head, pkgs...)
}

// RefreshArgs refreshes the package index before the first install (nil: m needs none).
func RefreshArgs(m Manager) []string {
	switch m {
	case Apt:
		return []string{"apt-get", "update", "-q"}
	case Apk:
		return []string{"apk", "update", "--no-progress"}
	}
	return nil
}

// Command is the line a person runs to install the items with m (sudo on Linux).
func Command(m Manager, items []Item) string {
	var pkgs []string
	seen := map[string]bool{}
	for _, it := range items {
		for _, p := range it.Pkgs[m] {
			if !seen[p] {
				seen[p] = true
				pkgs = append(pkgs, p)
			}
		}
	}
	args := InstallArgs(m, pkgs)
	if args == nil {
		return ""
	}
	if m == Brew {
		return strings.Join(args, " ")
	}
	return "sudo " + strings.Join(args, " ")
}
