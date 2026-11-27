// connect is Harnessly Connect: it links this computer to a harnessly runtime so the agents there run their
// commands here. Stage 1 is the console form; the tray comes on top of the same Device.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
)

var version = "dev"

const usage = `Harnessly Connect — даёт агентам harnessly доступ к этому компьютеру.

  connect pair --url https://harnessly.example.com [--name "MacBook"] hx_КОД
      подключить компьютер: код берётся в harnessly (harnessly connect code)
  connect run       держать связь и выполнять команды агентов (Ctrl+C: отключить и убить всё)
  connect status    к чему подключён
  connect forget    забыть подключение на этом компьютере
`

func main() {
	// Under the name harnessly it is the shim the agent's commands on this computer call.
	if n := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe"); n == "harnessly" {
		st, _ := os.Stdin.Stat()
		tty := st != nil && st.Mode()&os.ModeCharDevice != 0
		os.Exit(connect.RunShim(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, tty))
	}
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "pair":
		err = pair(os.Args[2:])
	case "run":
		err = run()
	case "status":
		err = status()
	case "forget":
		err = os.Remove(configPath())
	case "version", "--version":
		fmt.Println(version)
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

func pair(args []string) error {
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
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"code": fs.Arg(0), "name": *name, "os": runtime.GOOS, "arch": runtime.GOARCH, "pub": []byte(pub)})
	base := strings.TrimRight(*url, "/")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post(base+"/connect/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct{ ID, Name, Error string }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		if out.Error == "" {
			out.Error = resp.Status
		}
		return errors.New(out.Error)
	}
	cfg := connect.DeviceConfig{URL: base, ID: out.ID, Name: out.Name, Key: priv}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(configPath(), raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("подключено: %s (%s) → %s\n", out.Name, out.ID, base)
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
	c, err := load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d := &connect.Device{Cfg: c, Version: version, Logf: log.Printf,
		Status: func(online bool, why string) {
			if online {
				log.Printf("онлайн")
			} else {
				log.Printf("офлайн: %s", why)
			}
		}}
	err = d.Run(ctx)
	if errors.Is(err, context.Canceled) {
		log.Printf("отключено")
		return nil
	}
	return err
}
