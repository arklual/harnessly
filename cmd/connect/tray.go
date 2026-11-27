//go:build !darwin || cgo

package main

import (
	"fmt"
	"sync"

	"fyne.io/systray"
)

const hasTray = true

// runTray is Connect in the tray: its state, the window, «Отключить», the journal.
func runTray(a *app) {
	w, err := startWindow(a)
	if err != nil {
		a.log.Fatalf("окно: %v", err)
	}
	systray.Run(func() {
		systray.SetIcon(icon(false))
		systray.SetTooltip("Harnessly Connect")
		state := systray.AddMenuItem("", "")
		state.Disable()
		systray.AddSeparator()
		open := systray.AddMenuItem("Открыть окно…", "Подключение, состояние, автозапуск")
		toggle := systray.AddMenuItem("", "")
		journal := systray.AddMenuItem("Журнал", "Команды агентов и изменения файлов")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Выход", "Отключить и закрыть Connect")

		var mu sync.Mutex
		refresh := func() {
			mu.Lock()
			defer mu.Unlock()
			paired, name, _, running, online, why, commands := a.state()
			switch {
			case !paired:
				state.SetTitle("Не подключён")
				toggle.SetTitle("Подключить…")
			case online:
				state.SetTitle(fmt.Sprintf("Онлайн: %s · команд %d", name, commands))
				toggle.SetTitle("Отключить")
			case running:
				state.SetTitle("Нет связи: " + why)
				toggle.SetTitle("Отключить")
			default:
				state.SetTitle("Отключено")
				toggle.SetTitle("Подключить")
			}
			systray.SetIcon(icon(online))
			if online && commands > 0 {
				systray.SetTooltip(fmt.Sprintf("Harnessly Connect: агент работает (%d)", commands))
			} else {
				systray.SetTooltip("Harnessly Connect")
			}
		}
		a.onState = refresh
		refresh()
		if _, err := load(); err == nil {
			_ = a.start()
		} else {
			openURL(w.url)
		}
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					openURL(w.url)
				case <-toggle.ClickedCh:
					paired, _, _, running, _, _, _ := a.state()
					switch {
					case !paired:
						openURL(w.url)
					case running:
						a.stop()
					default:
						_ = a.start()
					}
					refresh()
				case <-journal.ClickedCh:
					openURL(journalPath())
				case <-quit.ClickedCh:
					a.stop()
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
}
