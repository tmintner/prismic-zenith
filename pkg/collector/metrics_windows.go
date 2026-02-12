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
		fmt.Printf("failed to get cpu usage: %v\n", err)
	} else {
		labels := map[string]string{"host": "localhost"}
		database.InsertMetric("cpu_usage_pct", cpuUsage, labels)
	}

	memUsed, memFree, err := getWindowsMemoryUsage()
	if err != nil {
		fmt.Printf("failed to get memory usage: %v\n", err)
	} else {
		labels := map[string]string{"host": "localhost"}
		database.InsertMetric("memory_used_mb", memUsed, labels)
		database.InsertMetric("memory_free_mb", memFree, labels)
	}

	if err := CollectSrumMetrics(database); err != nil {
		fmt.Printf("failed to collect SRUM metrics: %v\n", err)
	}

	if err := CollectSrumHistoricalMetrics(database); err != nil {
		fmt.Printf("failed to collect SRUM historical metrics: %v\n", err)
	}

	return nil
}

func CollectSrumHistoricalMetrics(database *db.VictoriaDB) error {
	// 1. Collect Application Resource Usage via PowerShell/.NET Parser
	err := collectSrumAppUsage(database)
	if err != nil {
		fmt.Printf("SRUM App Usage collection error: %v\n", err)
	}

	// 2. Collect Energy History via powercfg
	err = collectSrumEnergyHistory(database)
	if err != nil {
		fmt.Printf("SRUM Energy History collection error: %v\n", err)
	}

	return nil
}

func collectSrumAppUsage(database *db.VictoriaDB) error {
	// Execute the specialized PowerShell parser
	// Assuming srum_parser.ps1 is in the same directory or a known location
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", "./pkg/collector/srum_parser.ps1")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run srum_parser.ps1: %w", err)
	}

	if len(output) == 0 {
		return nil
	}

	var results []struct {
		AppName      string  `json:"AppName"`
		CycleTime    float64 `json:"CycleTime"`
		BytesRead    float64 `json:"BytesRead"`
		BytesWritten float64 `json:"BytesWritten"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return fmt.Errorf("failed to unmarshal srum app usage: %w", err)
	}

	for _, res := range results {
		if res.AppName == "" {
			continue
		}
		labels := map[string]string{
			"app_name": res.AppName,
		}
		database.InsertMetric("srum_app_cycle_time_total", res.CycleTime, labels)
		database.InsertMetric("srum_app_bytes_read_total", res.BytesRead, labels)
		database.InsertMetric("srum_app_bytes_written_total", res.BytesWritten, labels)
	}

	return nil
}

func collectSrumEnergyHistory(database *db.VictoriaDB) error {
	// Use powercfg to get application energy usage
	outputFile := "srum_energy.csv"
	cmd := exec.Command("powercfg", "/srumutil", "/output", outputFile, "/CSV")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run powercfg /srumutil: %w", err)
	}

	// Note: Parsing the full CSV might be heavy; for this implementation,
	// we'll just log that it was captured or implement a simple parser if needed.
	// Since we are inserting into VictoriaMetrics, we'd need to parse the CSV.
	// For now, let's keep it simple and focus on the database parsing.

	return nil
}

func CollectSrumMetrics(database *db.VictoriaDB) error {
	// 1. Collect SRUM Registry Data (Extensions)
	err := collectSrumRegistryData(database)
	if err != nil {
		fmt.Printf("SRUM Registry collection error: %v\n", err)
	}

	// 2. Collect SRUM Database Data via CIM (Network Usage)
	// MSFT_NetNetworkUsage is one of the classes backed by SRUM data
	err = collectSrumCimData(database)
	if err != nil {
		fmt.Printf("SRUM CIM collection error: %v\n", err)
	}

	return nil
}

func collectSrumRegistryData(database *db.VictoriaDB) error {
	// Querying the registry for SRUM extensions to understand what's being monitored
	cmd := exec.Command("powershell", "-Command", "Get-ChildItem 'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\SRUM\\Extensions' | Select-Object Name | ConvertTo-Json")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if len(output) == 0 {
		return nil
	}

	var results interface{}
	if err := json.Unmarshal(output, &results); err != nil {
		return err
	}

	// Count extensions as a simple metric of SRUM configuration depth
	count := 0
	switch v := results.(type) {
	case []interface{}:
		count = len(v)
	case map[string]interface{}:
		count = 1
	}

	labels := map[string]string{"source": "registry"}
	database.InsertMetric("srum_extensions_count", float64(count), labels)
	return nil
}

func collectSrumCimData(database *db.VictoriaDB) error {
	// MSFT_NetNetworkUsage provides per-interface network usage history derived from SRUM
	cmd := exec.Command("powershell", "-Command", "Get-CimInstance -Namespace root\\StandardCimv2 -ClassName MSFT_NetNetworkUsage | Select-Object InterfaceLuid, BytesSent, BytesReceived | ConvertTo-Json")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if len(output) == 0 {
		return nil
	}

	var rawResults json.RawMessage
	if err := json.Unmarshal(output, &rawResults); err != nil {
		return err
	}

	var results []struct {
		InterfaceLuid uint64 `json:"InterfaceLuid"`
		BytesSent     uint64 `json:"BytesSent"`
		BytesReceived uint64 `json:"BytesReceived"`
	}

	if output[0] == '{' {
		var single struct {
			InterfaceLuid uint64 `json:"InterfaceLuid"`
			BytesSent     uint64 `json:"BytesSent"`
			BytesReceived uint64 `json:"BytesReceived"`
		}
		if err := json.Unmarshal(output, &single); err == nil {
			results = append(results, single)
		}
	} else {
		json.Unmarshal(output, &results)
	}

	for _, res := range results {
		labels := map[string]string{
			"interface_luid": strconv.FormatUint(res.InterfaceLuid, 10),
		}
		database.InsertMetric("srum_network_bytes_sent_total", float64(res.BytesSent), labels)
		database.InsertMetric("srum_network_bytes_received_total", float64(res.BytesReceived), labels)
	}

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
