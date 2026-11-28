package hlcli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// connect manages Harnessly Connect: `harnessly connect code|devices|revoke <device>|folders|folder add|folder rm`.
func (c *cli) connect(args []string) error {
	if len(args) == 0 {
		args = []string{"devices"}
	}
	switch args[0] {
	case "code":
		// --url: the address the computer reaches this harnessly at, when the runtime has no RUNTIME_CONNECT_URL.
		q := url.Values{}
		if len(args) == 3 && args[1] == "--url" {
			q.Set("url", args[2])
		} else if len(args) != 1 {
			return fail("usage: harnessly connect code [--url https://адрес-harnessly]")
		}
		st, body, err := c.call(http.MethodPost, "/api/cli/connect/code", q, map[string]any{})
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fail("runtime answered %d: %s", st, body)
		}
		var out struct{ Code, Expires, Install, InstallWindows string }
		_ = json.Unmarshal(body, &out)
		until := ""
		if t, err := time.Parse(time.RFC3339, out.Expires); err == nil {
			until = " до " + t.Local().Format("15:04")
		}
		if out.Install == "" {
			c.out("%s\n", out.Code)
			fmt.Fprintf(c.env.Stderr, "код действует%s: connect pair --url <адрес harnessly> %s\n"+
				"команда установки появится с адресом: harnessly connect code --url https://… (или RUNTIME_CONNECT_URL)\n",
				until, out.Code)
			return nil
		}
		c.out("macOS, Linux (терминал):\n  %s\n\nWindows (PowerShell):\n  %s\n\nКод %s действует%s, на один компьютер.\n",
			out.Install, out.InstallWindows, out.Code, until)
		return nil
	case "devices", "list", "ls":
		st, body, err := c.call(http.MethodGet, "/api/cli/connect/devices", nil, nil)
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fail("runtime answered %d: %s", st, body)
		}
		var out struct {
			Devices []struct {
				ID, Name, OS, Arch string
				Online             bool
				Running            int
			}
		}
		_ = json.Unmarshal(body, &out)
		if len(out.Devices) == 0 {
			c.out("нет устройств: harnessly connect code\n")
		}
		for _, d := range out.Devices {
			state := "офлайн"
			if d.Online {
				state = "онлайн"
			}
			c.out("%s\t%s\t%s/%s\t%s\tкоманд: %d\n", d.ID, d.Name, d.OS, d.Arch, state, d.Running)
		}
		return nil
	case "folders":
		st, body, err := c.call(http.MethodGet, "/api/cli/connect/folders", nil, nil)
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fail("runtime answered %d: %s", st, body)
		}
		var out struct {
			Folders []struct{ Device, Dir string }
		}
		_ = json.Unmarshal(body, &out)
		if len(out.Folders) == 0 {
			c.out("нет папок устройств: harnessly connect folder add <устройство> <путь на нём>\n")
		}
		for _, f := range out.Folders {
			c.out("%s\t%s\n", f.Device, f.Dir)
		}
		return nil
	case "folder":
		var body map[string]string
		switch {
		case len(args) == 4 && args[1] == "add":
			body = map[string]string{"device": args[2], "dir": args[3]}
		case len(args) == 3 && (args[1] == "rm" || args[1] == "remove"):
			body = map[string]string{"op": "remove", "dir": args[2]}
		default:
			return fail("usage: harnessly connect folder add <device> <dir on it> | folder rm <dir>")
		}
		st, resp, err := c.call(http.MethodPost, "/api/cli/connect/folders", nil, body)
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			var e struct{ Error string }
			_ = json.Unmarshal(resp, &e)
			return fail("%s", e.Error)
		}
		if body["op"] == "remove" {
			c.out("папка убрана: %s\n", args[2])
		} else {
			c.out("чаты в %s теперь работают на устройстве %s\n", args[3], args[2])
		}
		return nil
	case "chat":
		return c.connectChat(args[1:])
	case "revoke":
		if len(args) != 2 {
			return fail("usage: harnessly connect revoke <device>")
		}
		st, body, err := c.call(http.MethodPost, "/api/cli/connect/revoke", nil, map[string]string{"device": args[1]})
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fail("runtime answered %d: %s", st, body)
		}
		c.out("отозвано: %s\n", args[1])
		return nil
	}
	return fail("usage: harnessly connect code|devices|revoke <device>|folders|folder add <device> <dir>|folder rm <dir>|chat <chat|current> [<device> <dir>|server]")
}

// connectChat says where one chat runs, or switches it: `chat <id|current>`, `chat <id|current> <device> <dir>`,
// `chat <id|current> server`. The agent starts again there once its turn is over.
func (c *cli) connectChat(args []string) error {
	if len(args) != 1 && len(args) != 2 && len(args) != 3 || len(args) == 2 && args[1] != "server" {
		return fail("usage: harnessly connect chat <chat|current> [<device> <dir on it> | server]")
	}
	who := map[string]string{"conv": args[0]}
	if args[0] == "current" {
		s := c.session()
		if s == "" {
			return errNoPane
		}
		who = map[string]string{"session": s}
	}
	if len(args) == 1 {
		q := url.Values{}
		for k, v := range who {
			q.Set(k, v)
		}
		st, body, err := c.call(http.MethodGet, "/api/cli/connect/chat", q, nil)
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return connectErr(st, body)
		}
		var out struct {
			On     *struct{ Device, Dir string }
			Folder bool
			Cwd    string
		}
		_ = json.Unmarshal(body, &out)
		switch {
		case out.On != nil:
			c.out("на устройстве %s: %s (здесь: %s)\n", out.On.Device, out.On.Dir, out.Cwd)
		case out.Folder:
			c.out("в папке устройства: %s\n", out.Cwd)
		default:
			c.out("на сервере: %s\n", out.Cwd)
		}
		return nil
	}
	body := map[string]string{}
	for k, v := range who {
		body[k] = v
	}
	if len(args) == 3 {
		body["device"], body["dir"] = args[1], args[2]
	}
	st, resp, err := c.call(http.MethodPost, "/api/cli/connect/chat", nil, body)
	if err != nil {
		return err
	}
	if st != http.StatusOK {
		return connectErr(st, resp)
	}
	var out struct{ Changed bool }
	_ = json.Unmarshal(resp, &out)
	switch {
	case !out.Changed:
		c.out("чат и так на сервере\n")
	case len(args) == 3:
		c.out("чат переключён на %s: %s — агент перезапустится там, когда закончит ход\n", args[1], args[2])
	default:
		c.out("чат возвращается на сервер — агент перезапустится, когда закончит ход\n")
	}
	return nil
}

func connectErr(st int, body []byte) error {
	var e struct{ Error string }
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return fail("%s", e.Error)
	}
	return fail("runtime answered %d: %s", st, body)
}
