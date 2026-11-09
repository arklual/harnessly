package hlcli

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// The person's agent memory, synced through the hub (docs/hub/memory-sync.md).

type memoryConflict struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Copy    string `json:"copy"`
	Machine string `json:"machine"`
}

type memoryReport struct {
	At        time.Time        `json:"at"`
	Projects  int              `json:"projects"`
	Pulled    []string         `json:"pulled"`
	Pushed    []string         `json:"pushed"`
	Deleted   []string         `json:"deleted"`
	Conflicts []memoryConflict `json:"conflicts"`
	Errors    []string         `json:"errors"`
}

func (c *cli) memory(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "status":
		raw, err := c.request("GET", "/api/cli/memory", nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var st struct {
			Enabled bool          `json:"enabled"`
			Last    *memoryReport `json:"last"`
		}
		if err := json.Unmarshal(raw, &st); err != nil {
			return err
		}
		if !st.Enabled {
			c.out("memory sync is off on this machine (RUNTIME_MEMORY_SYNC_URL is not set)\n")
			return nil
		}
		if st.Last == nil {
			c.out("memory sync is on; nothing synced yet\n")
			return nil
		}
		c.printMemoryReport(*st.Last)
		return nil
	case "sync":
		cwd := ""
		all := false
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--all":
				all = true
			case "--cwd":
				if i+1 >= len(args) {
					return usageErr("--cwd needs a folder")
				}
				i++
				cwd = args[i]
			default:
				return usageErr("memory sync takes --cwd DIR or --all")
			}
		}
		if !all {
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			if abs, err := filepath.Abs(cwd); err == nil {
				cwd = abs
			}
		}
		raw, err := c.request("POST", "/api/cli/memory/sync", url.Values{}, map[string]any{"cwd": cwd})
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var rep memoryReport
		if err := json.Unmarshal(raw, &rep); err != nil {
			return err
		}
		c.printMemoryReport(rep)
		if len(rep.Errors) > 0 {
			return fail("memory sync failed for some notes; the local notes are as they were, run it again")
		}
		return nil
	}
	return usageErr("memory takes status or sync: harnessly memory [status], harnessly memory sync [--cwd DIR | --all]")
}

func (c *cli) printMemoryReport(r memoryReport) {
	c.out("synced %s: %d projects, %d pulled, %d pushed, %d deleted, %d conflicts, %d errors\n",
		r.At.Local().Format("2006-01-02 15:04:05"), r.Projects, len(r.Pulled), len(r.Pushed), len(r.Deleted), len(r.Conflicts), len(r.Errors))
	if r.Projects == 0 {
		c.out("no project to sync (the folder has no git remote, or nothing synced before)\n")
	}
	for _, p := range r.Pulled {
		c.out("  ↓ %s\n", p)
	}
	for _, p := range r.Pushed {
		c.out("  ↑ %s\n", p)
	}
	for _, p := range r.Deleted {
		c.out("  ✕ %s\n", p)
	}
	for _, k := range r.Conflicts {
		c.out("  ! conflict %s %s: the other version (%s) is in %s; merge them and delete the copy\n", k.Project, k.Name, orDash(k.Machine), k.Copy)
	}
	for _, e := range r.Errors {
		c.out("  ! %s\n", e)
	}
}
