package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"time"

	"github.com/alex2108/systray"
)

func buildURL(path string, values url.Values) *url.URL {
	request, _ := url.Parse(config.URL + path)
	if values != nil {
		request.RawQuery = values.Encode()
	}
	return request
}

func buildFolderStateURL(values url.Values) *url.URL {
	return buildURL("/rest/db/status", values)
}

func buildConnectionsURL() *url.URL {
	return buildURL("/rest/system/connections", nil)
}

func buildStartTimeURL() *url.URL {
	return buildURL("/rest/system/status", nil)
}

func buildConfigURL() *url.URL {
	return buildURL("/rest/system/config", nil)
}

func buildVersionURL() *url.URL {
	return buildURL("/rest/system/version", nil)
}

func buildDeviceFolderCompletionURL(device string, folder string) *url.URL {
	values := url.Values{}
	values.Add("device", device)
	values.Add("folder", folder)
	return buildURL("/rest/db/completion", values)
}

func getFolderState() error {
	for key, rep := range folder {
		masterMutex.Lock()
		if folder[key].completion >= 0 {
			log.Println("already got info for folder", key, "from events, skipping")
			masterMutex.Unlock()
			continue
		}
		values := url.Values{}
		values.Add("folder", rep.id)
		query := buildFolderStateURL(values)
		response, err := querySyncthing(query.String())

		if err != nil {
			log.Println("error fetching folder info querySyncthing")
			log.Println("received reponse: " + response)
		}
		log.Println("getting state for folder", rep.id)
		if err == nil {
			type Folderstate struct {
				NeedFiles   int
				GlobalFiles int
				State       string
			}

			var m Folderstate
			jsonErr := json.Unmarshal([]byte(response), &m)

			if jsonErr != nil {
				log.Println("response: " + response)
				masterMutex.Unlock()
				return jsonErr
			}

			folder[key].state = m.State
			folder[key].needFiles = m.NeedFiles
			log.Println("needfiles", m.NeedFiles, "globalfiles", m.GlobalFiles)
			folder[key].completion = 100 - 100*float64(m.NeedFiles)/math.Max(float64(m.GlobalFiles), 1) // max to prevent division by zero
			log.Println("calculated completion%", folder[key].completion)

		} else {
			masterMutex.Unlock()
			return err
		}
		masterMutex.Unlock()
		// let events be processed, might save some expensive api calls
		for len(eventChan) > 0 {
			time.Sleep(time.Millisecond)
		}
	}

	return nil
}
func getConnections() error {
	masterMutex.Lock()
	defer masterMutex.Unlock()
	log.Println("getting connections")
	query := buildConnectionsURL()
	input, err := querySyncthing(query.String())
	if err != nil {
		log.Println(err)
		return err
	}
	var res map[string]interface{}
	err = json.Unmarshal([]byte(input), &res)

	for deviceID := range device {
		device[deviceID].connected = false
	}

	for deviceID, m := range res["connections"].(map[string]interface{}) {
		connectionState := m.(map[string]interface{})
		device[deviceID].connected = connectionState["connected"].(bool)
	}

	return err
}
func updateUl() error {

	type Completion struct {
		Completion float64
	}
	for folderName, folderInfo := range folder {
		for _, deviceName := range folderInfo.sharedWith {
			masterMutex.Lock()
			if device[deviceName].folderCompletion[folderName] >= 0 {
				log.Println("already got info for device", deviceName, "folder", folderName, "from events, skipping")
				masterMutex.Unlock()
				continue
			}

			if device[deviceName].connected { // only query connected devices
				query := buildDeviceFolderCompletionURL(deviceName, folderName)
				out, err := querySyncthing(query.String())
				log.Println("updating upload status for device", deviceName, "folder", folderName)
				if err != nil {
					log.Println(err)
					masterMutex.Unlock()
					return err
				}
				var m Completion
				err = json.Unmarshal([]byte(out), &m)
				if err != nil {
					log.Println(err)
					masterMutex.Unlock()
					return err
				}
				device[deviceName].folderCompletion[folderName] = m.Completion
			}
			masterMutex.Unlock()
			// let events be processed, might save some expensive api calls
			for len(eventChan) > 0 {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	return nil
}

func getStartTime() (string, error) {

	type StStatus struct {
		StartTime string
	}
	query := buildStartTimeURL()
	out, err := querySyncthing(query.String())

	if err != nil {
		log.Println(err)
		return "", err
	}
	var m StStatus
	err = json.Unmarshal([]byte(out), &m)
	if err != nil {
		log.Println(out)
		log.Println(err)
		return "", err
	}

	return m.StartTime, nil

}

// helper to get a lock before starting the new thread that can run in background after a lock is aquired
func initialize() {
	// block all before config is read
	log.Println("wating for lock")
	masterMutex.Lock()
	log.Println("wating for event lock")
	eventMutex.Lock()
	go initializeLocked()
}

func initializeLocked() {
	for isReady := false; !isReady; {
		currentStartTime, err := getStartTime()

		if err == nil {
			if startTime != currentStartTime {
				log.Println("syncthing restarted at", currentStartTime)
				startTime = currentStartTime
				sinceEvents = 0
			}
			err = getConfig()
			if err != nil {
				log.Println("error in getConfig")
			}
		} else {
			log.Println("error in getStartTime")
		}

		// clean out old events
		for len(eventChan) > 0 {
			select {
			case <-eventChan:
				continue
			default:
				continue
			}
		}
		masterMutex.Unlock()
		eventMutex.Unlock()

		// get current state
		if err == nil {
			err = getFolderState()
			if err != nil {
				log.Println("error in getFolderState")
			}
		}

		if err == nil {
			err = getConnections()
			if err != nil {
				log.Println("error in getConnections")
			}
		}

		if err == nil {
			err = updateUl()
			if err != nil {
				log.Println("error in updateUl")
			}
		}

		if err != nil {
			eventMutex.Lock()
			masterMutex.Lock()
			log.Println("error getting syncthing config -> retry in 5s", err)

			trayMutex.Lock()
			trayEntries.stVersion.SetTitle(fmt.Sprintf("Syncthing: no connection to " + config.URL))
			trayMutex.Unlock()

			systray.SetIcon(icon_error)
			time.Sleep(5 * time.Second)
		}

		isReady = (err == nil)
	}
	updateStatus()
}

func getConfig() error {
	log.Println("reading config from syncthing")
	//create empty state
	device = make(map[string]*Device)
	folder = make(map[string]*Folder)

	query := buildConfigURL()
	response, err := querySyncthing(query.String())

	if err != nil {
		log.Println("error in querySyncthing")
		log.Println("response was: " + response)
	}

	if err == nil {
		type SyncthingConfigDevice struct {
			DeviceID string
			Name     string
		}
		type SyncthingConfigFolderDevice struct {
			DeviceID string
		}

		type SyncthingConfigFolder struct {
			ID      string
			Devices []SyncthingConfigFolderDevice
		}
		type SyncthingConfig struct {
			Devices []SyncthingConfigDevice
			Folders []SyncthingConfigFolder
		}

		var m SyncthingConfig
		response := json.Unmarshal([]byte(response), &m)

		if response != nil {
			return response
		}

		// save config in structs
		// save Devices
		for _, v := range m.Devices {
			device[v.DeviceID] = &Device{v.Name, make(map[string]float64), false}
		}

		// save Folders
		for _, v := range m.Folders {
			folder[v.ID] = &Folder{v.ID, -1, "invalid", 0, make([]string, 0)} //id, completion, state, needFiles, sharedWith
			for _, v2 := range v.Devices {
				folder[v.ID].sharedWith = append(folder[v.ID].sharedWith, v2.DeviceID)
				device[v2.DeviceID].folderCompletion[v.ID] = -1
			}
		}

	} else {
		return err
	}

	//Display version
	log.Println("getting version")
	query = buildVersionURL()
	resp, err := querySyncthing(query.String())
	if err == nil {
		type STVersion struct {
			Version string
		}

		var m STVersion
		err = json.Unmarshal([]byte(resp), &m)
		if err == nil {
			log.Println("displaying version")
			trayMutex.Lock()
			trayEntries.stVersion.SetTitle(fmt.Sprintf("Syncthing: %s", m.Version))
			trayMutex.Unlock()

		}
	}
	return err
}
