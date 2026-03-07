//go:build windows

package collector

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"zenith/pkg/db"

	"github.com/Velocidex/ordereddict"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
	"www.velocidex.com/golang/go-ese/parser"
)

func CollectMetrics(database *db.VictoriaDB) error {
	type result struct {
		name string
		err  error
	}

	collectors := []struct {
		name string
		fn   func(*db.VictoriaDB) error
	}{
		{"CPU", collectCPUMetrics},
		{"Memory", collectMemoryMetrics},
		{"Process", CollectProcessMetrics},
		{"Network", collectNetworkMetrics},
		{"ProcessIO", collectProcessIOMetrics},
	}

	results := make(chan result, len(collectors))

	for _, c := range collectors {
		c := c // capture loop variable
		go func() {
			results <- result{c.name, c.fn(database)}
		}()
	}

	for range collectors {
		r := <-results
		if r.err != nil {
			fmt.Printf("failed to collect %s metrics: %v\n", r.name, r.err)
		}
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

		cpuPct, err := p.CPUPercent()
		if err == nil && cpuPct > 1.0 {
			database.InsertMetric("process_cpu_pct", cpuPct, labels)
		}
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

// collectProcessIOMetrics collects per-process disk I/O counters, duration, and
// user identity using Windows APIs via gopsutil every 5 minutes.
func collectProcessIOMetrics(database *db.VictoriaDB) error {
	procs, err := process.Processes()
	if err != nil {
		return err
	}

	nowMs := time.Now().UnixMilli()

	for _, p := range procs {
		ioStat, err := p.IOCounters()
		if err != nil {
			continue // skip processes we can't read
		}

		// Prefer full exe path; fall back to process name
		appName, err := p.Exe()
		if err != nil || appName == "" {
			appName, _ = p.Name()
		}
		if appName == "" {
			continue
		}
		appName = strings.ReplaceAll(appName, `\`, "/")

		// Resolve username for this process
		userName, _ := p.Username()
		if userName == "" {
			userName = "unknown"
		}
		// Strip domain prefix (DOMAIN\user → user)
		if idx := strings.LastIndex(userName, `\`); idx >= 0 {
			userName = userName[idx+1:]
		}

		// Compute duration from process create time
		createMs, err := p.CreateTime()
		var durationMs float64
		if err == nil {
			durationMs = float64(nowMs - createMs)
		}

		labels := map[string]string{
			"app_name":  appName,
			"user_name": userName,
		}

		if ioStat.ReadBytes > 0 || ioStat.WriteBytes > 0 {
			database.InsertMetric("srum_app_bytes_read_total", float64(ioStat.ReadBytes), labels)
			database.InsertMetric("srum_app_bytes_written_total", float64(ioStat.WriteBytes), labels)
		}
		if durationMs > 0 {
			database.InsertMetric("srum_app_duration_ms", durationMs, labels)
		}
	}
	return nil
}

const (
	srumDbPath           = "C:\\Windows\\System32\\sru\\SRUDB.dat"
	srumIdMapTable       = "SruDbIdMapTable" // Primary name
	srumIdMapTableAlt    = "SrumIdMapTable"  // Alternative name
	srumAppResourceTable = "{D10CA2FE-6FCF-4F6D-848E-B2E99266FA89}"
)

func CollectSrumHistoricalMetrics(database *db.VictoriaDB) (err error) {
	// Recover from panics in the third-party ESE parser
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("SRUM parsing panicked (likely dirty SRUDB.dat): %v", r)
		}
	}()

	// 1. Copy SRUDB.dat to a temporary location because it's locked by the system
	tempDir := os.TempDir()
	destPath := filepath.Join(tempDir, "SRUDB_zenith_copy.dat")

	// Ensure cleanup
	defer os.Remove(destPath)

	// Use WMI via PowerShell to create a VSS snapshot of C: (Supported on Windows 10/11 Client & Server)
	// This safely bypasses the DiagTrack exclusive lock on SRUDB.dat
	psScript := `$vss = (Get-WmiObject -List Win32_ShadowCopy).Create('C:\', 'ClientAccessible'); $shadow = Get-WmiObject Win32_ShadowCopy | Where-Object { $_.ID -eq $vss.ShadowID }; Write-Output ($shadow.DeviceObject + "|||" + $vss.ShadowID)`

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	outputBytes, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(outputBytes))

	if err != nil {
		fmt.Printf("warning: WMI failed to create VSS snapshot: %v, output: %s\n", err, output)
		// Fallback to direct raw copy
		if err := copyFile(srumDbPath, destPath); err != nil {
			return fmt.Errorf("failed to copy SRUDB.dat directly after VSS failure: %w", err)
		}
	} else {
		// Parse the newly created Shadow Copy Volume Name and ID
		parts := strings.Split(output, "|||")
		if len(parts) != 2 {
			fmt.Printf("warning: could not parse WMI VSS output: %s\n", output)
			if err := copyFile(srumDbPath, destPath); err != nil {
				return fmt.Errorf("failed to copy SRUDB.dat directly after VSS parse failure: %w", err)
			}
		} else {
			shadowVolumeRoot := strings.TrimSpace(parts[0])
			shadowID := strings.TrimSpace(parts[1])

			// Schedule cleanup of the specific VSS snapshot we just made
			defer func(id string) {
				cleanupScript := fmt.Sprintf(`(Get-WmiObject Win32_ShadowCopy | Where-Object { $_.ID -eq '%s' }).Delete()`, id)
				exec.Command("powershell", "-NoProfile", "-Command", cleanupScript).Run()
			}(shadowID)

			vssSrumPath := shadowVolumeRoot + `\Windows\System32\sru\SRUDB.dat`
			if err := copyFile(vssSrumPath, destPath); err != nil {
				return fmt.Errorf("failed to copy SRUDB.dat from VSS snapshot path %s: %w", vssSrumPath, err)
			}
		}
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

	// 4. Read SrumIdMapTable: split into app map (IdType=0) and user map (IdType=1)
	appIdMap := make(map[int32]string)  // appId  → app path
	userIdMap := make(map[int32]string) // userId → SID string

	firstRowPrinted := false
	parseSrumIdMap := func(row *ordereddict.Dict) error {
		if !firstRowPrinted {
			fmt.Printf("srum debug map keys: %v\n", row.Keys())
			firstRowPrinted = true
		}

		idVal, ok := getInt32FromDict(row, "IdIndex")
		if !ok {
			idVal, ok = getInt32FromDict(row, "Id")
		}
		if !ok {
			return nil
		}

		idType, _ := getInt32FromDict(row, "IdType")

		valStr, ok := row.Get("IdBlob")
		if !ok {
			valStr, ok = row.Get("Value")
		}
		if ok {
			if s, ok := valStr.(string); ok {
				name := decodeUTF16HexString(s)
				switch idType {
				case 1: // User SID
					userIdMap[idVal] = name
				default: // 0 = App path
					appIdMap[idVal] = sanitizeAppName(name)
				}
			}
		}
		return nil
	}

	err = catalog.DumpTable(srumIdMapTable, parseSrumIdMap)

	if err != nil {
		fmt.Printf("srum warning: %s not found, trying fallback %s\n", srumIdMapTable, srumIdMapTableAlt)
		err = catalog.DumpTable(srumIdMapTableAlt, parseSrumIdMap)
		if err != nil {
			fmt.Printf("srum warning: both %s and fallback %s not found in catalog\n", srumIdMapTable, srumIdMapTableAlt)
		}
	}
	fmt.Printf("srum debug: mapped %d app IDs, %d user IDs\n", len(appIdMap), len(userIdMap))

	// 5. Read Application Resource Usage Table
	metricsInserted := 0
	count := 0
	err = catalog.DumpTable(srumAppResourceTable, func(row *ordereddict.Dict) error {
		count++
		if count > 5000 {
			return io.EOF
		}

		appId, ok := getInt32FromDict(row, "AppId")
		if !ok {
			return nil
		}

		appName, exists := appIdMap[appId]
		if !exists || appName == "" {
			return nil
		}

		// Resolve user name
		userName := "unknown"
		if userId, ok := getInt32FromDict(row, "UserId"); ok {
			if sid, hasSid := userIdMap[userId]; hasSid && sid != "" {
				userName = sidToUsername(sid)
			}
		}

		cycleTime, _ := getInt64FromDict(row, "CycleTime")
		fgCycleTime, _ := getInt64FromDict(row, "ForegroundCycleTime")
		bgCycleTime, _ := getInt64FromDict(row, "BackgroundCycleTime")
		bytesRead, _ := getInt64FromDict(row, "BytesRead")
		bytesWritten, _ := getInt64FromDict(row, "BytesWritten")
		durationMs, _ := getInt64FromDict(row, "DurationMS")

		labels := map[string]string{
			"app_name":  appName,
			"user_name": userName,
		}

		database.InsertMetric("srum_app_cycle_time_total", float64(cycleTime), labels)
		database.InsertMetric("srum_app_bytes_read_total", float64(bytesRead), labels)
		database.InsertMetric("srum_app_bytes_written_total", float64(bytesWritten), labels)
		if fgCycleTime > 0 {
			database.InsertMetric("srum_app_foreground_cycle_time_total", float64(fgCycleTime), labels)
		}
		if bgCycleTime > 0 {
			database.InsertMetric("srum_app_background_cycle_time_total", float64(bgCycleTime), labels)
		}
		if durationMs > 0 {
			database.InsertMetric("srum_app_duration_ms", float64(durationMs), labels)
		}
		metricsInserted++
		return nil
	})

	fmt.Printf("srum debug: successfully inserted %d application metrics from %d parsed rows\n", metricsInserted, count)

	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to dump usage table: %w", err)
	}

	return nil
}

// sidToUsername resolves a Windows SID string (e.g. "S-1-5-21-...") to a
// human-readable account name using LookupAccountSid.
func sidToUsername(sidStr string) string {
	sid, err := windows.StringToSid(sidStr)
	if err != nil {
		// SID string might actually be a decoded blob that isn't a proper SID;
		// return the raw string trimmed of garbage.
		return strings.TrimSpace(sidStr)
	}
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return sidStr
	}
	if domain != "" {
		return domain + "\\" + account
	}
	return account
}

// decodeUTF16HexString converts a hex-encoded UTF-16LE string (as returned by
// go-ese for Long Binary columns in SRUDB.dat) to a regular UTF-8 Go string.
// If the input is not valid hex or UTF-16LE, the raw input is returned as-is.
func decodeUTF16HexString(s string) string {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) < 2 {
		return s // not hex-encoded, return raw
	}
	// Interpret as little-endian UTF-16 pairs
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	// Trim null terminators
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	if len(u16) == 0 {
		return s
	}
	return string(utf16.Decode(u16))
}

// sanitizeAppName cleans up an app name decoded from SRUM, stripping null bytes
// and normalising device paths to use forward slashes so they are safe as
// VictoriaMetrics label values.  Returns "" for obviously corrupted entries.
func sanitizeAppName(name string) string {
	name = strings.TrimSpace(name)
	// Strip embedded null bytes that remain after UTF-16 decoding
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Reject entries containing non-printable / binary garbage characters.
	// Valid app names consist of printable ASCII + common Unicode path chars.
	for _, r := range name {
		if r < 0x20 && r != 0x09 { // control chars except tab
			return ""
		}
	}

	// Normalise Windows device paths: replace backslashes with forward slashes.
	if strings.HasPrefix(name, `\`) {
		name = strings.ReplaceAll(name, `\`, "/")
	}

	// Reject suspiciously short device paths that are clearly truncated
	// (e.g. "/Device/" or "/Device/Har").
	if strings.HasPrefix(name, "/Device/") && len(name) < 20 {
		return ""
	}

	return name
}

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
