// Command windowseat-tray shows the running service's snapshot in the system tray.
// It only reads the service; it never starts a provider or the service itself.
package main

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --arch amd64,arm64 --out rsrc

import (
	"context"
	"flag"
	"log"
	"slices"
	"time"

	"fyne.io/systray"

	"windowseat/assets/icon"
	"windowseat/internal/contracts"
	"windowseat/internal/ipc"
	"windowseat/internal/tray"
)

const (
	pollInterval = 15 * time.Second
	callTimeout  = 5 * time.Second
)

func main() {
	warnAt := flag.Float64("warn-at", 80, "used percentage that turns the icon to warning")
	flag.Parse()

	addr, err := ipc.DefaultAddress()
	if err != nil {
		log.Fatal(err)
	}
	client := ipc.NewClient(addr)
	systray.Run(func() { go watch(client, *warnAt) }, nil)
}

// watch polls the service and redraws the icon and menu when they change.
func watch(client *ipc.Client, warnAt float64) {
	var shown tray.Status
	var menu []string
	refresh := make(chan struct{}, 1)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		status, lines := read(client, warnAt)
		if status != shown {
			showStatus(status)
			shown = status
		}
		if menu == nil || !slices.Equal(lines, menu) {
			buildMenu(lines, client, refresh)
			menu = lines
		}
		select {
		case <-ticker.C:
		case <-refresh:
		}
	}
}

func read(client *ipc.Client, warnAt float64) (tray.Status, []string) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return tray.Unreachable(), []string{err.Error()}
	}
	now := time.Now()
	return tray.Summarize(snapshot, warnAt, now), tray.MenuLines(snapshot, now, time.Local)
}

func showStatus(status tray.Status) {
	data, err := icon.Files.ReadFile(iconFile(status.Level))
	if err != nil {
		log.Printf("tray icon for %s: %v", status.Level, err)
	} else {
		systray.SetIcon(data)
	}
	systray.SetTooltip(status.Tooltip)
}

func buildMenu(lines []string, client *ipc.Client, refresh chan<- struct{}) {
	systray.ResetMenu()
	for _, line := range lines {
		systray.AddMenuItem(line, "").Disable()
	}
	systray.AddSeparator()
	onClick(systray.AddMenuItem("Refresh now", "Ask the service to read every provider"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := client.Refresh(ctx, contracts.Provider("")); err != nil {
			log.Printf("refresh: %v", err)
		}
		select {
		case refresh <- struct{}{}:
		default:
		}
	})
	onClick(systray.AddMenuItem("Quit tray", "The service keeps running"), systray.Quit)
}

// onClick runs do for each click until the menu is rebuilt and the item's channel closes.
func onClick(item *systray.MenuItem, do func()) {
	go func() {
		for range item.ClickedCh {
			do()
		}
	}()
}
