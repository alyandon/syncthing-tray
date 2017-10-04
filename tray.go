package main

import (
	"fmt"
	"log"
	"os"

	"github.com/alyandon/systray"
	"github.com/toqueteos/webbrowser"
)

func setMainTitle(message string) {
	trayEntries.mutex.Lock()
	defer trayEntries.mutex.Unlock()
	trayEntries.syncthingVersion.SetTitle(message)
}

func setRateTitle(message string) {
	trayEntries.mutex.Lock()
	defer trayEntries.mutex.Unlock()
	trayEntries.rateDisplay.SetTitle(message)
}

func setMainErrorTitle(message string) {
	trayEntries.mutex.Lock()
	defer trayEntries.mutex.Unlock()
	trayEntries.syncthingVersion.SetTitle(message)
	systray.SetIcon(icon_error)
}

func updateRateTitle(inBytesRate, outBytesRate float64) {
	setRateTitle("↓: " + formatRate(inBytesRate) + " ↑:" + formatRate(outBytesRate))
}

func updateConnectedDevicesTitle(numConnected int, downloading, uploading bool) {
	trayEntries.mutex.Lock()
	defer trayEntries.mutex.Unlock()
	trayEntries.connectedDevices.SetTitle(fmt.Sprintf("Connected to %d Devices", numConnected))
	updateIcon(numConnected, downloading, uploading)
}

func updateIcon(numConnected int, downloading, uploading bool) {
	if numConnected == 0 {
		//not connected
		log.Println("not connected")
		systray.SetIcon(icon_not_connected)
	} else if !downloading && !uploading {
		//idle
		log.Println("idle")
		systray.SetIcon(icon_idle)
	} else if downloading && uploading {
		//ul+dl
		log.Println("ul+dl")
		systray.SetIcon(icon_ul_dl)
	} else if downloading && !uploading {
		//dl
		log.Println("dl")
		systray.SetIcon(icon_dl)
	} else if !downloading && uploading {
		//ul
		log.Println("ul")
		systray.SetIcon(icon_ul)
	}
}

func spawnTrayEventLoop() {
	go func() {
		for {
			select {
			case <-trayEntries.quit.ClickedCh:
				log.Println("Quitting now...")
				// systray doesn't appear to be thread-safe (at least on darwin)
				log.Println("acquiring tray mutex")
				trayEntries.mutex.Lock()
				log.Println("tray mutex acquired - sending quit message")
				systray.Quit()
				log.Println("quit message sent - unlocking tray mutex")
				trayEntries.mutex.Unlock()
				log.Println("tray mutex unlocked - quitting program")
				os.Exit(0)
			case <-trayEntries.openBrowser.ClickedCh:
				webbrowser.Open(config.URL)
			}
		}

	}()
}

func setupTrayEntries() {
	trayEntries.mutex.Lock()
	defer trayEntries.mutex.Unlock()
	systray.SetIcon(icon_error)
	systray.SetTitle("")
	systray.SetTooltip("Syncthing-Tray")

	trayEntries.syncthingVersion = systray.AddMenuItem("not connected", "Syncthing")
	trayEntries.syncthingVersion.Disable()

	trayEntries.connectedDevices = systray.AddMenuItem("not connected", "Connected devices")
	trayEntries.connectedDevices.Disable()
	trayEntries.rateDisplay = systray.AddMenuItem("↓: 0 B/s ↑: 0 B/s", "Upload and download rate")
	trayEntries.rateDisplay.Disable()
	trayEntries.openBrowser = systray.AddMenuItem("Open Syncthing GUI", "opens syncthing GUI in default browser")

	trayEntries.quit = systray.AddMenuItem("Quit", "Quit Syncthing-Tray")
}
