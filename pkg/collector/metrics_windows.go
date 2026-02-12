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

func CollectMetrics(database *db.VictoriaDB) error {
	cpuUsage, err := getWindowsCPUUsage()
	if err != nil {
		return err
	}

	memUsed, memFree, err := getWindowsMemoryUsage()
	if err != nil {
		return err
	}

	labels := map[string]string{"host": "localhost"}
	database.InsertMetric("cpu_usage_pct", cpuUsage, labels)
	database.InsertMetric("memory_used_mb", memUsed, labels)
	database.InsertMetric("memory_free_mb", memFree, labels)

	return nil
}

func CollectProcessMetrics(database *db.VictoriaDB) error {
	// Use PowerShell to get per-process memory usage.
	// Calculating % CPU on Windows in a one-liner usually requires sampling over time,
	// so we'll focus on WorkingSet (Memory) for now to keep collection fast.
	cmd := exec.Command("powershell", "-Command", "Get-Process | Where-Object {$_.WorkingSet64 -gt 50MB} | Select-Object Id, ProcessName, WorkingSet64 | ConvertTo-Json")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if len(output) == 0 {
		return nil
	}

	var results []struct {
		Id           int     `json:"Id"`
		ProcessName  string  `json:"ProcessName"`
		WorkingSet64 float64 `json:"WorkingSet64"`
	}

	// Handle single vs array output from PowerShell
	if output[0] == '{' {
		var single struct {
			Id           int     `json:"Id"`
			ProcessName  string  `json:"ProcessName"`
			WorkingSet64 float64 `json:"WorkingSet64"`
		}
		if err := json.Unmarshal(output, &single); err == nil {
			results = append(results, single)
		}
	} else {
		json.Unmarshal(output, &results)
	}

	for _, p := range results {
		labels := map[string]string{
			"pid":          strconv.Itoa(p.Id),
			"process_name": p.ProcessName,
		}
		memoryMB := p.WorkingSet64 / (1024 * 1024)
		database.InsertMetric("process_memory_mb", memoryMB, labels)
	}

	return nil
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
