package hlcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/builders"
)

// The pool of builder machines and the heavy jobs of the hub (docs/hub/builders.md).

func (c *cli) builderClient() (*builders.Client, error) {
	dir := strings.TrimSpace(c.env.Getenv("HARNESSLY_DATA_DIR"))
	if dir == "" {
		dir = filepath.Join(c.env.Home, ".harnessly")
	}
	cl, err := builders.NewLocal(dir, strings.TrimSpace(c.env.Getenv("RUNTIME_AGENT_TOKEN_HEADER")))
	if err != nil {
		return nil, fail("%v: builders live in a hub, and this machine is not connected to one", err)
	}
	return cl, nil
}

func (c *cli) builders(args []string) error {
	if len(args) > 0 && args[0] != "list" && args[0] != "ls" {
		return usageErr("builders takes no arguments: harnessly builders [--json]")
	}
	cl, err := c.builderClient()
	if err != nil {
		return err
	}
	list, err := cl.Builders(context.Background())
	if err != nil {
		return err
	}
	if c.json {
		raw, _ := json.Marshal(map[string]any{"builders": list})
		c.printRaw(raw)
		return nil
	}
	if len(list) == 0 {
		c.out("no builders in the pool: heavy jobs run on this machine's own slots with draft limits.\nAn admin makes a machine a builder on the Machines page (role: builder).\n")
		return nil
	}
	for _, b := range list {
		state := "ready"
		if !b.Ready {
			state = "not ready"
			if b.Note != "" {
				state += " (" + b.Note + ")"
			}
		}
		c.out("%s\t%d/%d slots\t%s\t%s\twaiting %d\t%s\n", b.Name, b.Running, b.Slots, strings.Join(b.Caps, ","), state, b.Waiting, b.ID)
	}
	return nil
}

// jobFlags are the flags of `job run`.
type jobFlags struct {
	need, outs, env []string
	builder, in     string
	dest, title     string
	timeout         time.Duration
	argv            []string
}

func parseJobRun(args []string) (jobFlags, error) {
	f := jobFlags{timeout: time.Hour}
	val := func(i *int, name string) (string, error) {
		if *i+1 >= len(args) {
			return "", usageErr("%s needs a value", name)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			f.argv = args[i+1:]
			break
		}
		name, inline, hasInline := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "--") {
			f.argv = args[i:]
			break
		}
		get := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			return val(&i, name)
		}
		v, err := "", error(nil)
		switch name {
		case "--need", "--caps":
			if v, err = get(); err == nil {
				for _, p := range strings.Split(v, ",") {
					if p = strings.TrimSpace(p); p != "" {
						f.need = append(f.need, p)
					}
				}
			}
		case "--builder":
			f.builder, err = get()
		case "--in":
			f.in, err = get()
		case "--out":
			if v, err = get(); err == nil {
				f.outs = append(f.outs, v)
			}
		case "--dest":
			f.dest, err = get()
		case "--title":
			f.title, err = get()
		case "--env":
			if v, err = get(); err == nil {
				f.env = append(f.env, v)
			}
		case "--timeout":
			if v, err = get(); err == nil {
				if f.timeout, err = time.ParseDuration(v); err != nil || f.timeout <= 0 {
					return f, usageErr("--timeout is a duration like 20m or 2h")
				}
			}
		default:
			return f, usageErr("unknown flag %s", name)
		}
		if err != nil {
			return f, err
		}
	}
	if len(f.argv) == 0 {
		return f, usageErr("job run needs the command after the flags: harnessly job run --need blender --in DIR --out 'out/*.png' -- blender -b scene.blend -a")
	}
	return f, nil
}

func (c *cli) job(args []string) error {
	if len(args) == 0 {
		return usageErr("job takes run, list, status or cancel")
	}
	cl, err := c.builderClient()
	ctx := context.Background()
	if args[0] == "run" {
		return c.jobRun(ctx, cl, args[1:]) // cl is nil on a machine that is in no hub: the job runs here
	}
	if err != nil {
		return err
	}
	switch args[0] {
	case "list", "ls":
		jobs, err := cl.List(ctx, 20)
		if err != nil {
			return err
		}
		if c.json {
			raw, _ := json.Marshal(map[string]any{"jobs": jobs})
			c.printRaw(raw)
			return nil
		}
		for _, j := range jobs {
			c.out("%s\t%s\t%s\t%s\n", j.ID, j.Status, jobPlace(j), j.Title)
		}
		return nil
	case "status", "cancel":
		if len(args) != 2 {
			return usageErr("job %s JOB_ID", args[0])
		}
		var j builders.Job
		if args[0] == "cancel" {
			j, err = cl.Cancel(ctx, args[1])
		} else {
			j, err = cl.Get(ctx, args[1])
		}
		if err != nil {
			return err
		}
		if c.json {
			raw, _ := json.Marshal(map[string]any{"job": j})
			c.printRaw(raw)
			return nil
		}
		c.out("%s\t%s\t%s\n%s\n", j.ID, j.Status, jobPlace(j), j.Reason)
		if j.Error != "" {
			c.out("error: %s\n", j.Error)
		}
		for _, f := range j.Outputs {
			c.out("file: %s (%d bytes)\n", f.Path, f.Size)
		}
		return nil
	}
	return usageErr("job takes run, list, status or cancel")
}

func jobPlace(j builders.Job) string {
	switch {
	case j.Local:
		return "this machine (local slots)"
	case j.Builder != nil:
		return j.Builder.Name
	case j.Choice.Mode == "builder":
		return "waiting for " + j.Choice.Machine
	}
	return "queue (auto)"
}

func (c *cli) jobRun(ctx context.Context, cl *builders.Client, args []string) error {
	f, err := parseJobRun(args)
	if err != nil {
		return err
	}
	env := map[string]string{}
	for _, kv := range f.env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return usageErr("--env is NAME=value")
		}
		env[k] = v
	}
	in := f.in
	if in != "" {
		if !filepath.IsAbs(in) {
			in = filepath.Join(c.env.Cwd, in)
		}
		if st, err := os.Stat(in); err != nil || !st.IsDir() {
			return fail("--in %s is not a folder", f.in)
		}
	}
	dest := f.dest
	if dest != "" && !filepath.IsAbs(dest) {
		dest = filepath.Join(c.env.Cwd, dest)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	spec := builders.Spec{Title: f.title, Caps: f.need, Builder: f.builder, Argv: f.argv, Env: env, InDir: in,
		Outputs: f.outs, Dest: dest, Timeout: f.timeout, Log: c.env.Stderr}
	var res builders.Result
	if cl == nil {
		if spec.InDir == "" {
			spec.InDir = c.env.Cwd
		}
		res, err = builders.RunStandalone(ctx, spec, "this machine is not connected to a hub, so there is no pool of builders")
	} else {
		res, err = cl.Run(ctx, spec)
	}
	if c.json {
		raw, _ := json.Marshal(map[string]any{"job": res.Job, "files": res.Files, "exitCode": res.ExitCode, "ok": err == nil})
		c.printRaw(raw)
	} else {
		for _, p := range res.Files {
			c.out("%s\n", p)
		}
	}
	if err != nil {
		if res.ExitCode > 0 && res.ExitCode < 256 && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(c.env.Stderr, "harnessly: "+err.Error())
			return exitCode(res.ExitCode)
		}
		return err
	}
	return nil
}
