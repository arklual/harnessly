package apps

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// What an app reaches, as the person agrees to it (HRN-849): every right of its manifest, every secret, host, server of
// its own and part of a plugin it carries, each with its class of risk and a line in the person's words. The install
// screen lists them, the consent remembers their keys, and an update that adds a key waits for the person's word.

// Access is one thing an app reaches.
type Access struct {
	// Key names it for the consent: perm:<right>, secret:<name>, net:<host>, frame:<host>, server:<name>, part:<part>.
	Key string `json:"key"`
	// Code is how the manifest writes it; the screen shows it small, on hover.
	Code   string `json:"code"`
	Class  string `json:"class"`
	Label  Text   `json:"label"`
	Threat Text   `json:"threat,omitzero"`
}

var (
	threatCommands = Text{Ru: "Сможет выполнять команды на вашей машине.", En: "It can run commands on your machine."}
	threatAgent    = Text{Ru: "Сможет видеть и останавливать действия агента.", En: "It can see and stop what the agent does."}
	threatRemote   = Text{Ru: "Код окна живёт на сервере {param} и может меняться без новой версии.", En: "The view's code lives on {param} and can change without a new version."}
)

// surfacePermission is the right of the surface a manifest's right names (files.read for files.read:src/**).
func surfacePermission(right string) (SurfacePermission, string, bool) {
	name, param, _ := strings.Cut(right, ":")
	for _, p := range surfacePermissions {
		if p.Name == name {
			return p, param, true
		}
	}
	return SurfacePermission{}, "", false
}

func (t Text) with(param string) Text {
	return Text{Ru: strings.ReplaceAll(t.Ru, "{param}", param), En: strings.ReplaceAll(t.En, "{param}", param)}
}

// AccessOf is what the manifest (and its package, when it can be read) reaches, from the most dangerous; nil pkg
// leaves out the servers and the plugin parts.
func AccessOf(m *Manifest, pkg *Package) []Access {
	if m == nil {
		return []Access{}
	}
	out := []Access{}
	add := func(a Access) {
		if !slices.ContainsFunc(out, func(o Access) bool { return o.Key == a.Key }) {
			out = append(out, a)
		}
	}
	for _, right := range m.Permissions {
		p, param, ok := surfacePermission(right)
		if !ok {
			continue
		}
		if p.Name == "secrets" {
			add(secretAccess(param))
			continue
		}
		if label, ok := eventLabels[param]; ok && p.Name == "events" {
			add(Access{Key: "perm:" + right, Code: right, Class: p.Class, Label: label})
			continue
		}
		// The two rights that spend say how much: the key carries it, so a version that spends more asks again.
		key := "perm:" + right
		switch p.Name {
		case "ai":
			param = Money(BudgetOf(m).USDPerDay)
			key += "@" + param
		case "agents.run":
			param = fmt.Sprint(BudgetOf(m).RunsPerDay)
			key += "@" + param
		}
		label := p.Label.with(param)
		if p.Name == "agents.run" {
			n := BudgetOf(m).RunsPerDay
			label = Text{Ru: "Запускать агента в новом чате — до " + timesRu(n) + " в день", En: "Start the agent in a new chat — up to " + timesEn(n) + " a day"}
		}
		class, threat := p.Class, p.Threat.with(param)
		if param != "" && p.ParamClass != "" {
			class = p.ParamClass
		}
		if param == "" && p.Alone.Ru != "" {
			label = p.Alone
		}
		if !Loud(class) {
			threat = Text{}
		}
		add(Access{Key: key, Code: right, Class: class, Label: label, Threat: threat})
	}
	for _, s := range m.Secrets {
		add(secretAccess(s.Name))
	}
	if n := m.Network; n != nil {
		for _, h := range slices.Concat(n.Connect, n.Resources) {
			add(Access{Key: "net:" + h, Code: "network: " + h, Class: ClassMedium, Label: netLabel(h)})
		}
		for _, h := range n.Frames {
			add(Access{Key: "frame:" + h, Code: "network.frames: " + h, Class: ClassHigh,
				Label:  Text{Ru: "Показывать окно с сервера {param}", En: "Show a view from {param}"}.with(h),
				Threat: threatRemote.with(h)})
		}
	}
	if pkg != nil {
		names := make([]string, 0, len(pkg.MCP))
		for name := range pkg.MCP {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			s := pkg.MCP[name]
			if s.Command != "" {
				add(Access{Key: "server:" + name, Code: ".mcp.json: " + name, Class: ClassHigh,
					Label:  Text{Ru: "Запускать свою программу {param} на вашей машине", En: "Run its program {param} on your machine"}.with(name),
					Threat: threatCommands})
				continue
			}
			host := name
			if u, err := url.Parse(s.URL); err == nil && u.Host != "" {
				host = u.Host
			}
			add(Access{Key: "server:" + name, Code: ".mcp.json: " + name, Class: ClassMedium,
				Label: Text{Ru: "Подключать агенту свой сервер {param}", En: "Connect its server {param} to the agent"}.with(host)})
		}
		for _, part := range pkg.PluginParts {
			add(partAccess(part))
		}
	}
	slices.SortStableFunc(out, func(a, b Access) int { return slices.Index(Classes, a.Class) - slices.Index(Classes, b.Class) })
	return out
}

// eventLabels say each kind of events:<kind> in words.
var eventLabels = map[string]Text{
	"turn":    {Ru: "Узнавать, когда агент закончил ход", En: "Know when the agent finishes a turn"},
	"tool":    {Ru: "Узнавать, какие инструменты вызывает агент", En: "Know which tools the agent calls"},
	"message": {Ru: "Узнавать о новых сообщениях в чате", En: "Know when a message comes in the chat"},
}

func secretAccess(name string) Access {
	return Access{Key: "secret:" + name, Code: "secrets:" + name, Class: ClassHigh,
		Label:  Text{Ru: "Использовать секрет {param}", En: "Use the secret {param}"}.with(name),
		Threat: Text{Ru: "Сможет действовать от вашего имени там, куда ведёт этот ключ.", En: "It can act on your behalf wherever this key leads."}}
}

func netLabel(host string) Text {
	if host == "*" {
		return Text{Ru: "Ходить в сеть на любые адреса", En: "Reach any address on the network"}
	}
	return Text{Ru: "Ходить в сеть на {param}", En: "Reach {param} on the network"}.with(host)
}

// partAccess is a part of a Claude Code plugin: hooks see every action of the agent and can stop it, the rest runs
// commands or adds to the agent.
func partAccess(part string) Access {
	a := Access{Key: "part:" + part, Code: part, Class: ClassHigh, Threat: threatCommands}
	switch low := strings.ToLower(part); {
	case strings.HasPrefix(low, "hooks") || strings.HasSuffix(low, "plugin.json: hooks"):
		a.Class, a.Threat = ClassCritical, threatAgent
		a.Label = Text{Ru: "Видеть действия агента и останавливать их", En: "See the agent's actions and stop them"}
	case strings.HasPrefix(low, "agent.validators"):
		a.Label = Text{Ru: "Проверять ход агента своими скриптами на вашей машине", En: "Check the agent's turn with its scripts on your machine"}
	case strings.HasPrefix(low, "agents"):
		a.Class, a.Threat = ClassMedium, Text{}
		a.Label = Text{Ru: "Добавлять агенту своих помощников", En: "Add helpers of its own to the agent"}
	default:
		a.Label = Text{Ru: "Добавлять в агента часть плагина {param}", En: "Add the plugin part {param} to the agent"}.with(part)
	}
	return a
}

// AccessKeys are the keys of a list.
func AccessKeys(list []Access) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Key)
	}
	return out
}

// NewAccess are the items of next whose keys granted lacks: what an update adds.
func NewAccess(next []Access, granted []string) []Access {
	out := []Access{}
	for _, a := range next {
		if !slices.Contains(granted, a.Key) {
			out = append(out, a)
		}
	}
	return out
}

// timesRu is «N раз» as Russian counts it after «до»: до 1 раза, до 3 раз, до 21 раза.
func timesRu(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return fmt.Sprint(n) + " раза"
	}
	return fmt.Sprint(n) + " раз"
}

func timesEn(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprint(n) + " times"
}
