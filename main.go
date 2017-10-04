package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/alex2108/systray"
	"github.com/toqueteos/webbrowser"
)

// VersionStr build version
var VersionStr = "unknown"

// BuildUnixTime build timestamp
var BuildUnixTime = "0"

var masterMutex sync.Mutex
var eventMutex sync.Mutex

var sinceEvents = 0
var startTime = "-"
var eventChan = make(chan event)

type dataRates struct {
	mutex        sync.Mutex
	inBytesRate  float64
	outBytesRate float64
}

func (transferRates *dataRates) SetRates(inBytesRate float64, outBytesRate float64) {
	transferRates.mutex.Lock()
	defer transferRates.mutex.Unlock()
	transferRates.inBytesRate = inBytesRate
	transferRates.outBytesRate = outBytesRate
}

func (transferRates *dataRates) ReadRates() (float64, float64) {
	transferRates.mutex.Lock()
	defer transferRates.mutex.Unlock()
	return transferRates.inBytesRate, transferRates.outBytesRate
}

var transferRates dataRates

type folderSummary struct {
	NeedFiles   int    `json:"needFiles"`
	State       string `json:"state"`
	GlobalFiles int    `json:"globalFiles"`
	NeedDeletes int    `json:"needDeletes"`
}

type eventData struct {
	Folder     string        `json:"folder"`
	Summary    folderSummary `json:"summary"`
	Completion float64       `json:"completion"`
	Device     string        `json:"device"`
	ID         string        `json:"id"`
}
type event struct {
	ID   int       `json:"id"`
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data eventData `json:"data"`
}

// Config for connection to syncthing
type Config struct {
	URL      string
	APIKey   string
	insecure bool
	useRates bool
}

var config Config

// Device represents configured devices
type Device struct {
	name             string
	folderCompletion map[string]float64
	connected        bool
}

var device map[string]*Device

// Folder represents configured folders
type Folder struct {
	id         string
	completion float64
	state      string
	needFiles  int
	sharedWith []string
}

var folder map[string]*Folder

func buildEventsURL(values url.Values) *url.URL {
	return buildURL("/rest/events", values)
}

func queryEvents() ([]event, error) {
	loggedEventLock("queryEvents")
	defer loggedEventUnlock("queryEvents")
	values := url.Values{}
	values.Add("since", strconv.Itoa(sinceEvents))
	query := buildEventsURL(values)
	response, err := querySyncthing(query.String())

	if err != nil {
		log.Println("Events query failed", query.String(), response, err)
		return err
	}

	var events []event
	err = json.Unmarshal([]byte(response), &events)
	if err != nil {
		log.Println("Parsing events failed", query.String(), response, err)
		return err
	}

	for _, event := range events {
		eventChan <- event
		sinceEvents = event.ID
		log.Println("Sent event ID", event.ID)
	}

	return nil
}

func eventProcessor() {
	for event := range eventChan {
		loggedMasterLock("eventProcessor") // mutex with initialitze which may still be running
		// handle different events
		needUpdateStatus := true
		switch event.Type {
		case "FolderSummary":
			folder[event.Data.Folder].needFiles = event.Data.Summary.NeedFiles
			folder[event.Data.Folder].state = event.Data.Summary.State
			log.Println("NeedDeletes", event.Data.Summary.NeedDeletes)
			if event.Data.Summary.NeedDeletes == 0 {
				log.Println("foldersummary folder", event.Data.Folder, "needfiles", event.Data.Summary.NeedFiles, "globalfiles", event.Data.Summary.GlobalFiles)
				folder[event.Data.Folder].completion = 100 - 100*float64(event.Data.Summary.NeedFiles)/math.Max(float64(event.Data.Summary.GlobalFiles), 1)
				log.Println("foldersummary calculated completion%", folder[event.Data.Folder].completion)
			} else {
				folder[event.Data.Folder].completion = 95
			}
		case "FolderCompletion":
			device[event.Data.Device].folderCompletion[event.Data.Folder] = event.Data.Completion

		case "DeviceConnected":
			log.Println(event.Data.ID, "connected")
			device[event.Data.ID].connected = true

		case "DeviceDisconnected":
			log.Println(event.Data.ID, "disconnected")
			device[event.Data.ID].connected = false

		case "ConfigSaved":
			log.Println("got new config -> reinitialize")
			sinceEvents = event.ID
			loggedMasterUnlock("eventProcessor ConfigSaved")
			initialize()
			continue
		default:
			log.Println("ignoring event type", event.Type, event.Data.Folder)
			needUpdateStatus = false
		}

		if needUpdateStatus {
			updateStatus()
		}

		loggedMasterUnlock("eventProcessor")
	}
}

func pollEvents() {
	for {
		err := readEvents()

		// attempt to re-initialize if an error occurs
		if err != nil {
			initialize()
		}

		// don't hammer the api
		time.Sleep(500 * time.Millisecond)
	}

}

func updateStatus() {
	log.Println("updating status")

	downloading := false
	uploading := false
	numConnected := 0

	for _, folderInfo := range folder {
		//log.Printf("folder %v",fol)
		//log.Printf("folderInfo %v",folderInfo)
		if folderInfo.completion < 100 {
			downloading = true
		}
	}

	for _, deviceInfo := range device {
		//log.Printf("device %v",dev)
		//log.Printf("device_info %v",deviceInfo)

		if deviceInfo.connected {
			numConnected++

			for folderName, completion := range deviceInfo.folderCompletion {
				if completion < 100 {
					uploading = true
					log.Println("DEBUG:", deviceInfo.name, folderName, completion)
				}
			}
		}
	}

	log.Printf("connected %v", numConnected)

	if config.useRates {
		inBytesRate, outBytesRate := transferRates.ReadRates()
		downloading = inBytesRate > 500
		uploading = outBytesRate > 500
	}

	updateConnectedDevicesTitle(numConnected, downloading, uploading)
}

func main() {
	// must be done at the beginning
	systray.Run(setupTray)
}

// TrayEntries contains values to display in the system tray
type TrayEntries struct {
	mutex            sync.Mutex
	syncthingVersion *systray.MenuItem
	connectedDevices *systray.MenuItem
	rateDisplay      *systray.MenuItem
	openBrowser      *systray.MenuItem
	quit             *systray.MenuItem
}

var trayEntries TrayEntries

func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		systray.Quit()
		os.Exit(0)
	}()
}

func parseOptions() {
	url := flag.String("target", "http://localhost:8384", "Target Syncthing instance")
	api := flag.String("api", "", "Syncthing Api Key (used for password protected syncthing instance)")
	insecure := flag.Bool("i", false, "skip verification of SSL certificate")
	useRates := flag.Bool("R", false, "use transfer rates to determine upload/download state")
	flag.Parse()

	if *api == "" {
		log.Println("api key is a required parameter")
		os.Exit(1)
	}

	config.URL = *url
	config.APIKey = *api
	config.insecure = *insecure
	config.useRates = *useRates
}

func setupLogging() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	buildInt, _ := strconv.Atoi(BuildUnixTime)
	buildT := time.Unix(int64(buildInt), 0)
	date := buildT.UTC().Format("2006-01-02 15:04:05 MST")
	log.Println("Starting Syncthing-Tray", VersionStr, "-", date)
	log.Println("Connecting to syncthing at", config.URL)
}

func spawnWorkers() {
	go rateReader()
	go eventProcessor()
	go func() {
		initialize()
		pollEvents()
	}()
}

func setupTray() {
	parseOptions()
	setupSignalHandler()
	setupLogging()
	spawnWorkers()
	setupTrayEntries()
}

func onClick() { // not usable on ubuntu, left click also displays the menu
	fmt.Println("Opening webinterface in browser")
	webbrowser.Open(config.URL)
}
