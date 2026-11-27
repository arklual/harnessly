package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func agentPlist() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "tech.harnessly.connect.plist")
}

func autostartOn() bool { _, err := os.Stat(agentPlist()); return err == nil }

// setAutostart: a LaunchAgent starts Connect when the person logs in.
func setAutostart(on bool) error {
	if !on {
		err := os.Remove(agentPlist())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>tech.harnessly.connect</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Interactive</string>
</dict></plist>
`, exe)
	_ = os.MkdirAll(filepath.Dir(agentPlist()), 0o755)
	return os.WriteFile(agentPlist(), []byte(plist), 0o644)
}
