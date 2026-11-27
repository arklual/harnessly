package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func desktopFile() string {
	return filepath.Join(configHome(), "autostart", "harnessly-connect.desktop")
}
func unitFile() string {
	return filepath.Join(configHome(), "systemd", "user", "harnessly-connect.service")
}

func graphical() bool { return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" }

func autostartOn() bool {
	if _, err := os.Stat(desktopFile()); err == nil {
		return true
	}
	_, err := os.Stat(unitFile())
	return err == nil
}

// setAutostart: an XDG autostart entry on a desktop, a systemd user service on a server without one.
func setAutostart(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !on {
		_ = os.Remove(desktopFile())
		if _, err := os.Stat(unitFile()); err == nil {
			_ = exec.Command("systemctl", "--user", "disable", "--now", "harnessly-connect.service").Run()
			_ = os.Remove(unitFile())
		}
		return nil
	}
	if graphical() {
		_ = os.MkdirAll(filepath.Dir(desktopFile()), 0o755)
		return os.WriteFile(desktopFile(), []byte(fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Harnessly Connect\n"+
			"Exec=%q\nX-GNOME-Autostart-enabled=true\nNoDisplay=false\n", exe)), 0o644)
	}
	_ = os.MkdirAll(filepath.Dir(unitFile()), 0o755)
	unit := fmt.Sprintf("[Unit]\nDescription=Harnessly Connect\nAfter=network-online.target\n\n[Service]\nExecStart=%q run\n"+
		"Restart=always\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n", exe)
	if err := os.WriteFile(unitFile(), []byte(unit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return exec.Command("systemctl", "--user", "enable", "harnessly-connect.service").Run()
}
