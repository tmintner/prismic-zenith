//go:build darwin

package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
	"zenith/pkg/db"
)

// LogShowEntry represents the structure of the JSON output from `log show`
type LogShowEntry struct {
	Timestamp    string `json:"timestamp"`
	ProcessID    int    `json:"processID"`
	ProcessName  string `json:"processImagePath"`
	Subsystem    string `json:"subsystem"`
	Category     string `json:"category"`
	LogLevel     int    `json:"messageType"`
	EventMessage string `json:"eventMessage"`
}

func CollectLogs(database *db.VictoriaDB, duration string) error {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		dur = 5 * time.Minute
	}

	// Calculate the last N minutes/hours for `log show`
	// `log show` uses a specific format for --last
	lastArg := fmt.Sprintf("%ds", int(dur.Seconds()))

	cmd := exec.Command("log", "show", "--last", lastArg, "--style", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run log show: %v", err)
	}

	if len(output) == 0 {
		return nil
	}

	var rawEntries []LogShowEntry
	if err := json.Unmarshal(output, &rawEntries); err != nil {
		return fmt.Errorf("failed to parse log JSON: %v", err)
	}

	if len(rawEntries) == 0 {
		return nil
	}

	var logs []db.LogEntry
	for _, raw := range rawEntries {
		logs = append(logs, db.LogEntry{
			Timestamp:    raw.Timestamp,
			ProcessName:  raw.ProcessName,
			Category:     raw.Category,
			LogLevel:     fmt.Sprintf("%d", raw.LogLevel),
			EventMessage: raw.EventMessage,
		})
	}

	if len(logs) > 0 {
		if err := database.InsertLogs(logs); err != nil {
			return fmt.Errorf("failed to insert logs: %v", err)
		}
	}

	return nil
}
