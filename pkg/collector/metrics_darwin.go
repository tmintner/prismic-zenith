//go:build darwin

package collector

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
	"zenith/pkg/db"
)

func CollectMetrics(database *db.Database) error {
	// Simple performance metrics using top and vm_stat
	// This is a simplified version; real-world collection would be more robust.

	cpuUsage, err := getCPUUsage()
	if err != nil {
		return err
	}

	memUsed, memFree, err := getMemoryUsage()
	if err != nil {
		return err
	}

	_, err = database.Conn.Exec(
		"INSERT INTO system_metrics (timestamp, cpu_usage_pct, memory_used_mb, memory_free_mb) VALUES (?, ?, ?, ?)",
		time.Now().UTC().Format("2006-01-02 15:04:05"), cpuUsage, memUsed, memFree,
	)

	return err
}

func CollectProcessMetrics(database *db.Database) error {
	// Use ps to get per-process CPU and memory usage
	// ps -axo pid,comm,%cpu,rss
	cmd := exec.Command("ps", "-axo", "pid,comm,%cpu,rss")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Skip header line and parse each process
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		processName := fields[1]
		cpuPct, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}

		// RSS is in KB on macOS, convert to MB
		rssKB, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		memoryMB := rssKB / 1024.0

		// Only store processes using significant resources (>0.1% CPU or >10MB RAM)
		if cpuPct > 0.1 || memoryMB > 10.0 {
			_, err = database.Conn.Exec(
				"INSERT INTO process_metrics (timestamp, pid, process_name, cpu_pct, memory_mb) VALUES (?, ?, ?, ?, ?)",
				timestamp, pid, processName, cpuPct, memoryMB,
			)
			if err != nil {
				// Log but don't fail the entire collection
				continue
			}
		}
	}

	return nil
}

func getCPUUsage() (float64, error) {
	// Fallback to average load since `top` is restricted in this environment.
	// uptime | awk '{print $10}'
	cmd := exec.Command("uptime")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	// uptime output example: 20:13  up 1 day, 20:13, 2 users, load averages: 1.83 2.05 2.10
	parts := strings.Split(string(output), "load averages:")
	if len(parts) < 2 {
		return 0, nil
	}
	loads := strings.Fields(parts[1])
	if len(loads) > 0 {
		return strconv.ParseFloat(strings.TrimSuffix(loads[0], ","), 64)
	}
	return 0, nil
}

func getMemoryUsage() (float64, float64, error) {
	// vm_stat
	cmd := exec.Command("vm_stat")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	lines := strings.Split(string(output), "\n")
	var freePages, activePages float64
	pageSize := 4096.0 // Standard macOS page size, could be dynamic

	for _, line := range lines {
		if strings.Contains(line, "Pages free:") {
			parts := strings.Fields(line)
			freePages, _ = strconv.ParseFloat(strings.TrimSuffix(parts[2], "."), 64)
		}
		if strings.Contains(line, "Pages active:") {
			parts := strings.Fields(line)
			activePages, _ = strconv.ParseFloat(strings.TrimSuffix(parts[2], "."), 64)
		}
	}

	freeMB := (freePages * pageSize) / (1024 * 1024)
	usedMB := (activePages * pageSize) / (1024 * 1024)

	return usedMB, freeMB, nil
}
