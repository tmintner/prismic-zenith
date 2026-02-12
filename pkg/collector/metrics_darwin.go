//go:build darwin

package collector

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"zenith/pkg/db"
)

func CollectMetrics(database *db.VictoriaDB) error {
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

	labels := map[string]string{"host": "localhost"}
	if err := database.InsertMetric("cpu_usage_pct", cpuUsage, labels); err != nil {
		return err
	}
	if err := database.InsertMetric("memory_used_mb", memUsed, labels); err != nil {
		return err
	}
	if err := database.InsertMetric("memory_free_mb", memFree, labels); err != nil {
		return err
	}

	return nil
}

func CollectProcessMetrics(database *db.VictoriaDB) error {
	// Use ps to get per-process CPU and memory usage.
	// We put comm at the end so it can contain spaces.
	// ps -axo pid,rss,%cpu,comm
	cmd := exec.Command("ps", "-axo", "pid,rss,%cpu,comm")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")

	// Skip header line and parse each process
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}

		// Parse from the left for fixed fields
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid := fields[0]
		rssKB, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		cpuPct, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}

		// The rest of the line is the command (comm)
		// We re-extract it to handle spaces properly
		idIdx := strings.Index(line, fields[0])
		rssIdx := strings.Index(line[idIdx+len(fields[0]):], fields[1]) + idIdx + len(fields[0])
		cpuIdx := strings.Index(line[rssIdx+len(fields[1]):], fields[2]) + rssIdx + len(fields[1])
		comm := strings.TrimSpace(line[cpuIdx+len(fields[2]):])

		processName := filepath.Base(comm)
		memoryMB := rssKB / 1024.0

		// Only store processes using significant resources (>1.0% CPU or >50MB RAM)
		if cpuPct > 1.0 || memoryMB > 50.0 {
			labels := map[string]string{
				"pid":          pid,
				"process_name": processName,
			}
			database.InsertMetric("process_cpu_pct", cpuPct, labels)
			database.InsertMetric("process_memory_mb", memoryMB, labels)
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
