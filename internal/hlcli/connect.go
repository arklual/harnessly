package hlcli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// connect manages Harnessly Connect devices: `harnessly connect code|devices|revoke <device>`.
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
	return fail("usage: harnessly connect code|devices|revoke <device>")
}
