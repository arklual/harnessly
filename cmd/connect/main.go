// connect is Harnessly Connect: it links this computer to a harnessly runtime so the agents there run their
// commands here. Stage 1 is the console form; the tray comes on top of the same Device.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/connect"
)

var version = "dev"

const usage = `Harnessly Connect — даёт агентам harnessly доступ к этому компьютеру.

  connect           значок в трее и окно (подключение, состояние, «Отключить», журнал)
  connect pair --url https://harnessly.example.com [--name "MacBook"] hx_КОД
      подключить компьютер: код берётся в harnessly (harnessly connect code)
  connect run       без трея: держать связь в терминале (Ctrl+C: отключить и убить всё)
  connect status    к чему подключён
  connect forget    забыть подключение на этом компьютере
  connect autostart on|off   запускать при входе в систему
`

func main() {
	// Under the name harnessly it is the shim the agent's commands on this computer call.
	if n := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe"); n == "harnessly" {
		st, _ := os.Stdin.Stat()
		tty := st != nil && st.Mode()&os.ModeCharDevice != 0
		os.Exit(connect.RunShim(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, tty))
	}
	if len(os.Args) < 2 {
		// A Linux server has no tray: Connect keeps the link in the terminal (or as a systemd user service).
		if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			if err := run(); err != nil {
				fmt.Fprintln(os.Stderr, "connect:", err)
				os.Exit(1)
			}
			return
		}
		runTray(newApp())
		return
	}
	var err error
	switch os.Args[1] {
	case "pair":
		err = pairCmd(os.Args[2:])
	case "run":
		err = run()
	case "status":
		err = status()
	case "forget":
		err = os.Remove(configPath())
	case "autostart":
		if len(os.Args) != 3 || (os.Args[2] != "on" && os.Args[2] != "off") {
			fmt.Print(usage)
			os.Exit(2)
		}
		err = setAutostart(os.Args[2] == "on")
	case "version", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
}

func configPath() string {
	if p := os.Getenv("HARNESSLY_CONNECT_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "harnessly-connect", "device.json")
}

func load() (connect.DeviceConfig, error) {
	var c connect.DeviceConfig
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return c, errors.New("компьютер ещё не подключён: connect pair --url … hx_КОД")
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}

func pairCmd(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	url := fs.String("url", "", "адрес harnessly")
	host, _ := os.Hostname()
	name := fs.String("name", host, "имя компьютера в harnessly")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || fs.NArg() != 1 {
		return errors.New("нужны --url и код hx_…")
	}
	cfg, err := pair(*url, fs.Arg(0), *name)
	if err != nil {
		return err
	}
	fmt.Printf("подключено: %s (%s) → %s\n", cfg.Name, cfg.ID, cfg.URL)
	return nil
}

func status() error {
	c, err := load()
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s) → %s\n", c.Name, c.ID, c.URL)
	return nil
}

func run() error {
	a := newApp()
	if err := a.start(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.onState = func() {
		_, _, _, _, online, why, _ := a.state()
		if online {
			a.log.Printf("онлайн")
		} else {
			a.log.Printf("офлайн: %s", why)
		}
	}
	<-ctx.Done()
	a.stop()
	a.log.Printf("отключено")
	return nil
}
