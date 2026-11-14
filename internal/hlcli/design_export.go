package hlcli

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// `harnessly design export`: one page of a project as one HTML file that opens anywhere, or the project as a
// zip, made by the project's own exporters and handed to the person in the chat (deliver) unless --no-deliver.

const designExportPath = "/api/cli/design/export"

// exportPoll is how often the export is asked about; exportWait how long it is waited for.
var (
	exportPoll = time.Second
	exportWait = 10 * time.Minute
)

type exportAnswer struct {
	Job struct {
		ID    string  `json:"id"`
		State string  `json:"state"`
		Error *string `json:"error"`
	} `json:"job"`
	Path string `json:"path"`
}

func (c *cli) designExport(args []string) error {
	args, on := switches(args, "zip", "all", "no-deliver")
	f, err := parseFlags(args, nil)
	if err != nil {
		return err
	}
	format := "html"
	switch {
	case on["all"]:
		format = "bundle"
	case on["zip"]:
		format = "zip"
	}
	if len(f.pos) < 1 || len(f.pos) > 2 || (format == "html") != (len(f.pos) == 2) {
		return usageErr("design export <project> <page> | design export <project> --zip|--all [--no-deliver]")
	}
	body := map[string]any{"project": f.pos[0], "format": format}
	if len(f.pos) == 2 {
		body["page"] = f.pos[1]
	}
	raw, err := c.request(http.MethodPost, designExportPath, nil, body)
	if err != nil {
		return err
	}
	var out exportAnswer
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	deadline := time.Now().Add(exportWait)
	for out.Path == "" {
		switch out.Job.State {
		case "failed":
			msg := "the export failed"
			if out.Job.Error != nil {
				msg = *out.Job.Error
			}
			return usageErr("%s", msg)
		case "done":
			if out.Path == "" {
				return usageErr("the export is done but its file is gone")
			}
		}
		if time.Now().After(deadline) {
			return usageErr("the export did not finish in %s", exportWait)
		}
		time.Sleep(exportPoll)
		raw, err = c.request(http.MethodGet, designExportPath, url.Values{"project": {f.pos[0]}, "job": {out.Job.ID}}, nil)
		if err != nil {
			return err
		}
		out = exportAnswer{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
	}
	if on["no-deliver"] || c.session() == "" {
		c.out("%s\n", out.Path)
		return nil
	}
	return c.deliver([]string{out.Path})
}
