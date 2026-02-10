//go:build darwin

package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"zenith/pkg/db"
)

func CollectLogs(database *db.Database, duration string) error {
	cmd := exec.Command("log", "show", "--last", duration, "--style", "json", "--info")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run log show: %v", err)
	}

	var logs []LogEntry
	if err := json.Unmarshal(output, &logs); err != nil {
		return fmt.Errorf("failed to unmarshal logs: %v", err)
	}

	for _, entry := range logs {
		_, err := database.Conn.Exec(
			"INSERT INTO system_logs (timestamp, process, subsystem, category, level, message) VALUES (?, ?, ?, ?, ?, ?)",
			entry.Timestamp, entry.ProcessName, entry.Subsystem, entry.Category, entry.LogLevel, entry.EventMessage,
		)
		if err != nil {
			return fmt.Errorf("failed to insert log: %v", err)
		}
	}

	return nil
}
