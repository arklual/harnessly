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
	return fail("usage: harnessly connect code|devices|revoke <device>|folders|folder add <device> <dir>|folder rm <dir>")
}
