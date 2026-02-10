//go:build windows

package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"zenith/pkg/db"
)

func CollectMetrics(database *db.Database) error {
	cpuUsage, err := getWindowsCPUUsage()
	if err != nil {
		return err
	}

	memUsed, memFree, err := getWindowsMemoryUsage()
	if err != nil {
		return err
	}

	_, err = database.Conn.Exec(
		"INSERT INTO system_metrics (timestamp, cpu_usage_pct, memory_used_mb, memory_free_mb) VALUES (?, ?, ?, ?)",
		time.Now().Format("2006-01-02 15:04:05"), cpuUsage, memUsed, memFree,
	)

	return err
}

func getWindowsCPUUsage() (float64, error) {
	// typeperf "\Processor(_Total)\% Processor Time" -sc 1
	cmd := exec.Command("typeperf", `\Processor(_Total)\% Processor Time`, "-sc", "1")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, ",") && !strings.Contains(line, "(PDH-CSV") {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				return strconv.ParseFloat(val, 64)
			}
		}
	}
	return 0, fmt.Errorf("failed to parse CPU usage from typeperf")
}

func getWindowsMemoryUsage() (float64, float64, error) {
	// Using systeminfo or wmic could be slow. Let's use powershell for memory.
	cmd := exec.Command("powershell", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object FreePhysicalMemory, TotalVisibleMemorySize | ConvertTo-Json")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	// Simplified parsing for brevity in this example
	var result struct {
		FreePhysicalMemory     float64 `json:"FreePhysicalMemory"`
		TotalVisibleMemorySize float64 `json:"TotalVisibleMemorySize"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return 0, 0, err
	}

	freeMB := result.FreePhysicalMemory / 1024
	totalMB := result.TotalVisibleMemorySize / 1024
	usedMB := totalMB - freeMB

	return usedMB, freeMB, nil
}
