package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

func rateReader() {
	var prevInBytes int64
	var prevOutBytes int64
	var rateInterval int64 = 10

	for range time.Tick(time.Duration(rateInterval) * time.Second) {
		inBytes, outBytes, err := readRate()
		if err != nil {
			prevInBytes = 0
			prevInBytes = 0
		}

		inBytesRate := float64(inBytes-prevInBytes) / float64(rateInterval)
		outBytesRate := float64(outBytes-prevOutBytes) / float64(rateInterval)
		log.Println("inBytesRate:", formatRate(inBytesRate), "outBytesRate:", formatRate(outBytesRate))

		transferRates.SetRates(inBytesRate, outBytesRate)

		prevInBytes = inBytes
		prevOutBytes = outBytes
		updateRateTitle(inBytesRate, outBytesRate)

		if config.useRates {
			masterMutex.Lock()
			updateStatus()
			masterMutex.Unlock()
		}
	}
}

func formatRate(rate float64) string {
	if rate < 1024 { // 1 KiB
		return fmt.Sprintf("%.2f B/s", rate)
	} else if rate < 1024*1024 { // 1MiB
		return fmt.Sprintf("%.2f KiB/s", rate/1024)
	}
	return fmt.Sprintf("%.2f MiB/s", rate/(1024*1024))
}

func readRate() (int64, int64, error) {

	type connState struct {
		Connected     bool   `json:"connected"`
		InBytesTotal  int64  `json:"inBytesTotal"`
		OutBytesTotal int64  `json:"outBytesTotal"`
		At            string `json:"at"`
	}

	type restConn struct {
		Total       connState            `json:"total"`
		Connections map[string]connState `json:"connections"`
	}

	query := buildConnectionsURL()
	input, err := querySyncthing(query.String())
	if err != nil {
		log.Println("connection query failed", err)
		return 0, 0, err
	}
	var res restConn
	err = json.Unmarshal([]byte(input), &res)

	return res.Total.InBytesTotal, res.Total.OutBytesTotal, nil
}
