//go:build darwin

package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"zenith/pkg/db"
)

func CollectLogs(database *db.VictoriaDB, duration string) error {
	cmd := exec.Command("log", "show", "--last", duration, "--style", "json", "--info")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run log show: %v", err)
	}

	var logs []db.LogEntry
	if err := json.Unmarshal(output, &logs); err != nil {
		return fmt.Errorf("failed to unmarshal logs: %v", err)
	}

	if err := database.InsertLogs(logs); err != nil {
		return fmt.Errorf("failed to insert logs: %v", err)
	}

	return nil
}
