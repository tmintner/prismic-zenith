//go:build windows

package collector

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"zenith/pkg/db"

	"github.com/Velocidex/ordereddict"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"www.velocidex.com/golang/go-ese/parser"
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

	if err := collectNetworkMetrics(database); err != nil {
		fmt.Printf("failed to collect network metrics: %v\n", err)
	}

	// SRUM is heavy, maybe run it less frequently or just catch errors without spamming
	if err := CollectSrumHistoricalMetrics(database); err != nil {
		fmt.Printf("failed to collect SRUM historical metrics: %v\n", err)
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
		// Filter out processes with low memory usage to reduce noise
		memInfo, err := p.MemoryInfo()
		if err != nil || memInfo.RSS < 50*1024*1024 { // 50MB
			continue
		}

		name, err := p.Name()
		if err != nil {
			name = "unknown"
		}

		labels := map[string]string{
			"pid":          strconv.Itoa(int(p.Pid)),
			"process_name": name,
		}
		database.InsertMetric("process_memory_mb", float64(memInfo.RSS)/1024/1024, labels)
	}
	return nil
}

func collectNetworkMetrics(database *db.VictoriaDB) error {
	counters, err := net.IOCounters(true) // per interface
	if err != nil {
		return err
	}

	for _, c := range counters {
		labels := map[string]string{
			"interface": c.Name,
		}
		database.InsertMetric("srum_network_bytes_sent_total", float64(c.BytesSent), labels)
		database.InsertMetric("srum_network_bytes_received_total", float64(c.BytesRecv), labels)
	}
	return nil
}

// SRUM Collection Implementation

const (
	srumDbPath           = "C:\\Windows\\System32\\sru\\SRUDB.dat"
	srumIdMapTable       = "SrumIdMapTable"
	srumAppResourceTable = "{D10CA2FE-6FCF-4F6D-848E-B2E99266FA89}"
)

func CollectSrumHistoricalMetrics(database *db.VictoriaDB) error {
	// 1. Copy SRUDB.dat to a temporary location because it's locked by the system
	tempDir := os.TempDir()
	destPath := filepath.Join(tempDir, "SRUDB_zenith_copy.dat")

	// Ensure cleanup
	defer os.Remove(destPath)

	if err := copyFile(srumDbPath, destPath); err != nil {
		return fmt.Errorf("failed to copy SRUDB.dat: %w", err)
	}

	// 2. Create ESE Context
	f, err := os.Open(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	eseCtx, err := parser.NewESEContext(f)
	if err != nil {
		return fmt.Errorf("failed to create ESE context: %w", err)
	}

	// 3. Read Database Catalog
	catalog, err := parser.ReadCatalog(eseCtx)
	if err != nil {
		return fmt.Errorf("failed to read ESE catalog: %w", err)
	}

	// 4. Read SrumIdMapTable: Id (Int32) -> Value (String, the App Name)
	idMap := make(map[int32]string)

	err = catalog.DumpTable(srumIdMapTable, func(row *ordereddict.Dict) error {
		idVal, ok := getInt32FromDict(row, "Id")
		if !ok {
			return nil
		}

		valStr, ok := row.Get("Value")
		if ok {
			if s, ok := valStr.(string); ok {
				idMap[idVal] = s
			}
		}
		return nil
	})
	if err != nil {
		fmt.Printf("warning: failed to dump SrumIdMapTable: %v\n", err)
	}

	// 5. Read Application Resource Usage Table
	// Limit processing to recent entries or just top consumers to avoid heavy load
	count := 0
	err = catalog.DumpTable(srumAppResourceTable, func(row *ordereddict.Dict) error {
		count++
		if count > 500 { // Limit for safety in this demo
			return io.EOF // Stop iteration
		}

		// Extract fields
		appId, _ := getInt32FromDict(row, "AppId")
		cycleTime, _ := getInt64FromDict(row, "CycleTime")
		bytesRead, _ := getInt64FromDict(row, "BytesRead")
		bytesWritten, _ := getInt64FromDict(row, "BytesWritten")

		appName, exists := idMap[appId]
		if !exists || appName == "" {
			return nil
		}

		labels := map[string]string{
			"app_name": appName,
		}

		// These are accumulating counters in SRUM, so we just report current total
		database.InsertMetric("srum_app_cycle_time_total", float64(cycleTime), labels)
		database.InsertMetric("srum_app_bytes_read_total", float64(bytesRead), labels)
		database.InsertMetric("srum_app_bytes_written_total", float64(bytesWritten), labels)
		return nil
	})

	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to dump usage table: %w", err)
	}

	return nil
}

// copyFile is a helper to copy a file
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func getInt32FromDict(m *ordereddict.Dict, key string) (int32, bool) {
	v, ok := m.Get(key)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int32:
		return t, true
	case int64:
		return int32(t), true
	case int:
		return int32(t), true
	default:
		return 0, false
	}
}

func getInt64FromDict(m *ordereddict.Dict, key string) (int64, bool) {
	v, ok := m.Get(key)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case int:
		return int64(t), true
	default:
		return 0, false
	}
}
