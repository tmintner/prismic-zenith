//go:build darwin

package collector

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"zenith/pkg/db"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

func CollectMetrics(database *db.VictoriaDB) error {
	if err := collectCPUMetrics(database); err != nil {
		fmt.Printf("failed to collect CPU metrics: %v\n", err)
	}

	if err := collectMemoryMetrics(database); err != nil {
		fmt.Printf("failed to collect memory metrics: %v\n", err)
	}

	if err := CollectProcessMetrics(database); err != nil {
		fmt.Printf("failed to collect process metrics: %v\n", err)
	}

	return nil
}

func collectCPUMetrics(database *db.VictoriaDB) error {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return err
	}
	if len(percent) > 0 {
		labels := map[string]string{"host": "localhost"}
		return database.InsertMetric("cpu_usage_pct", percent[0], labels)
	}
	return nil
}

func collectMemoryMetrics(database *db.VictoriaDB) error {
	v, err := mem.VirtualMemory()
	if err != nil {
		return err
	}

	labels := map[string]string{"host": "localhost"}
	database.InsertMetric("memory_used_mb", float64(v.Used)/1024/1024, labels)
	database.InsertMetric("memory_free_mb", float64(v.Free)/1024/1024, labels)
	return nil
}

func CollectProcessMetrics(database *db.VictoriaDB) error {
	procs, err := process.Processes()
	if err != nil {
		return err
	}

	for _, p := range procs {
		memInfo, err := p.MemoryInfo()
		if err != nil || memInfo.RSS < 50*1024*1024 { // 50MB
			continue
		}

		name, err := p.Name()
		if err != nil {
			name = "unknown"
		}

		// Clean up name if it's a full path
		name = filepath.Base(name)

		labels := map[string]string{
			"pid":          strconv.Itoa(int(p.Pid)),
			"process_name": name,
		}
		database.InsertMetric("process_memory_mb", float64(memInfo.RSS)/1024/1024, labels)

		cpuPct, err := p.CPUPercent()
		if err == nil && cpuPct > 1.0 {
			database.InsertMetric("process_cpu_pct", cpuPct, labels)
		}
	}
	return nil
}
