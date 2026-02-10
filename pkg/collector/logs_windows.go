//go:build windows

package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"zenith/pkg/db"
)

type WinEvent struct {
	TimeCreated      string `json:"TimeCreated"`
	Id               int    `json:"Id"`
	Level            int    `json:"Level"`
	LevelDisplayName string `json:"LevelDisplayName"`
	Message          string `json:"Message"`
	ProviderName     string `json:"ProviderName"`
}

func CollectLogs(database *db.Database, duration string) error {
	// duration format is like "5m", "1h". PowerShell needs a DateTime or similar.
	// We'll use Get-WinEvent with a FilterHashtable for better performance.

	// Simplify: just get recent Application and System logs
	psCommand := fmt.Sprintf(`Get-WinEvent -FilterHashtable @{LogName='Application','System'; StartTime=(Get-Date).AddMinutes(-%s)} -ErrorAction SilentlyContinue | Select-Object TimeCreated, Id, Level, LevelDisplayName, Message, ProviderName | ConvertTo-Json`, parseDurationToMinutes(duration))

	cmd := exec.Command("powershell", "-Command", psCommand)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Get-WinEvent returns exit status 1 if no events match the filter.
		// We should treat this as "no events" rather than a hard failure.
		if strings.Contains(outputStr, "No events were found") || strings.Contains(outputStr, "NoEventFound") {
			return nil
		}
		return fmt.Errorf("failed to run Get-WinEvent: %v (output: %s)", err, outputStr)
	}

	if len(output) == 0 {
		return nil
	}

	var events []WinEvent
	// PowerShell might return a single object instead of an array if only one event is found
	if output[0] == '{' {
		var single WinEvent
		if err := json.Unmarshal(output, &single); err != nil {
			return fmt.Errorf("failed to unmarshal single event: %v", err)
		}
		events = append(events, single)
	} else {
		if err := json.Unmarshal(output, &events); err != nil {
			return fmt.Errorf("failed to unmarshal events: %v", err)
		}
	}

	for _, event := range events {
		_, err := database.Conn.Exec(
			"INSERT INTO system_logs (timestamp, process, subsystem, category, level, message) VALUES (?, ?, ?, ?, ?, ?)",
			event.TimeCreated, event.ProviderName, "", fmt.Sprintf("ID: %d", event.Id), event.LevelDisplayName, event.Message,
		)
		if err != nil {
			return fmt.Errorf("failed to insert Windows log: %v", err)
		}
	}

	return nil
}

func parseDurationToMinutes(duration string) string {
	// Handle plain numbers as minutes
	val, err := strconv.Atoi(duration)
	if err == nil {
		return strconv.Itoa(val)
	}

	if len(duration) < 2 {
		return "5" // default
	}

	valStr := duration[:len(duration)-1]
	unit := duration[len(duration)-1]

	v, err := strconv.Atoi(valStr)
	if err != nil {
		return "5"
	}

	switch unit {
	case 'm':
		return valStr
	case 'h':
		return strconv.Itoa(v * 60)
	default:
		return "5"
	}
}
