package hlcli

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// harnessly app public: the public catalog (internal/apps/public.go).
//
//	app public [list [QUERY]]       what anyone may install
//	app public publish ID VERSION   put the version on stable in it (the checks run on the machine; a wide network waits for a look)
//	app public install ID [--yes]   show the rights, then install for yourself
func (c *cli) appPublic(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls", "search":
		raw, err := c.request("GET", "/api/cli/apps/public", url.Values{"q": {strings.Join(args, " ")}}, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var out struct {
			Apps []apps.PublicEntry `json:"apps"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if len(out.Apps) == 0 {
			c.out("nothing in the public catalog for this\n")
			return nil
		}
		for _, e := range out.Apps {
			c.out("%s\t%s\t%s\t%s\n", e.ID, e.Version, e.Name, e.Description)
		}
		return nil
	case "publish":
		if len(args) != 2 {
			return usageErr("app public publish ID VERSION  (the version must be the one on stable)")
		}
		status, raw, err := c.call("POST", "/api/cli/apps/public/publish", nil, map[string]string{"id": args[0], "version": args[1]})
		if err != nil {
			return err
		}
		if status >= 300 {
			return shareError(status, raw)
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var e apps.PublicEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Review == apps.ReviewPending {
			c.out("%s %s is sent for a look (%s); it is listed when a person at the platform approves it\n", e.ID, e.Version, strings.Join(e.Why, ", "))
		} else {
			c.out("%s %s is in the public catalog\n", e.ID, e.Version)
		}
		return nil
	case "install":
		vals, rest, err := appFlags(args, "yes")
		if err != nil {
			return err
		}
		if err := allowFlags(vals, "yes"); err != nil {
			return err
		}
		if len(rest) != 1 {
			return usageErr("app public install ID [--yes]")
		}
		raw, err := c.request("GET", "/api/cli/apps/public", url.Values{"q": {rest[0]}}, nil)
		if err != nil {
			return err
		}
		var found struct {
			Apps []apps.PublicEntry `json:"apps"`
		}
		if err := json.Unmarshal(raw, &found); err != nil {
			return err
		}
		var e *apps.PublicEntry
		for i := range found.Apps {
			if found.Apps[i].ID == rest[0] {
				e = &found.Apps[i]
			}
		}
		if e == nil {
			return fail("%s is not in the public catalog", rest[0])
		}
		// The rights are what the person agrees to: they are shown before anything is installed.
		if vals["yes"] != "true" {
			c.out("%s %s (%s) asks for: %s\n", e.ID, e.Version, e.Trust, rightsLine(e.Permissions))
			c.out("to install: harnessly app public install --yes %s\n", e.ID)
			return nil
		}
		status, raw, err := c.call("POST", "/api/cli/apps/public/install", nil, map[string]string{"id": e.ID, "sum": e.Sum})
		if err != nil {
			return err
		}
		if status >= 300 {
			return shareError(status, raw)
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		c.out("%s %s is installed\n", e.ID, e.Version)
		return nil
	}
	return usageErr("app public takes list, publish or install")
}

func rightsLine(perms []string) string {
	if len(perms) == 0 {
		return "no rights"
	}
	return strings.Join(perms, ", ")
}
