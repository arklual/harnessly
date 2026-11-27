package hlcli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// connect manages Harnessly Connect: `harnessly connect code|devices|revoke <device>|folders|folder add|folder rm`.
func (c *cli) connect(args []string) error {
	if len(args) == 0 {
		args = []string{"devices"}
	}
	switch args[0] {
	case "code":
		st, body, err := c.call(http.MethodPost, "/api/cli/connect/code", nil, map[string]any{})
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fail("runtime answered %d: %s", st, body)
		}
		var out struct{ Code, Expires string }
		_ = json.Unmarshal(body, &out)
		c.out("%s\n", out.Code)
		if t, err := time.Parse(time.RFC3339, out.Expires); err == nil {
			fmt.Fprintf(c.env.Stderr, "действует до %s: connect pair --url <адрес harnessly> %s\n", t.Local().Format("15:04"), out.Code)
		}
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
		var out struct{ Folders []struct{ Device, Dir string } }
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
