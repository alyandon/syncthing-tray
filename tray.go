package main

import (
	"fmt"
	"sync"
)

var trayMutex sync.Mutex

func updateRateTitle(inBytesRate, outBytesRate float64) {
	trayMutex.Lock()
	trayEntries.rateDisplay.SetTitle("↓: " + formatRate(inBytesRate) + " ↑:" + formatRate(outBytesRate))
	trayMutex.Unlock()
}

func updateConnectedDevicesTitle(numConnected int, downloading, uploading bool) {
	trayMutex.Lock()
	trayEntries.connectedDevices.SetTitle(fmt.Sprintf("Connected to %d Devices", numConnected))
	setIcon(numConnected, downloading, uploading)
	trayMutex.Unlock()
}
