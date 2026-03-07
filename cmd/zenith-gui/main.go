package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"zenith/pkg/config"
	"zenith/pkg/db"
	"zenith/pkg/guiassets"

	webview "github.com/webview/webview_go"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		fmt.Printf("Warning: Failed to load config.json, using defaults: %v\n", err)
		cfg = &config.Config{
			ServerHost:  "localhost",
			ServerPort:  8080,
			MetricsHost: "localhost",
			MetricsPort: 8428,
		}
	}

	serverURL := fmt.Sprintf("http://%s:%d", cfg.ServerHost, cfg.ServerPort)
	metricsURL := fmt.Sprintf("http://%s:%d", cfg.MetricsHost, cfg.MetricsPort)
	victoria := db.NewVictoriaDB(metricsURL, "")

	// Compose HTML with embedded CSS and JS
	composedHTML := guiassets.ComposeHTML()

	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle("Zenith System Monitor")
	w.SetSize(1100, 750, webview.HintNone)

	// Bind Go functions to JavaScript
	w.Bind("getGreeting", func() map[string]string {
		hostname, _ := os.Hostname()
		return map[string]string{
			"hostname": hostname,
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
		}
	})

	w.Bind("getSystemMetrics", func() map[string]interface{} {
		result := map[string]interface{}{}

		// CPU usage
		cpuResult, err := victoria.QueryMetrics("cpu_usage_pct")
		if err != nil {
			result["error"] = err.Error()
			return result
		}
		result["cpu_pct"] = parseFirstValue(cpuResult)

		// Memory
		memUsed, _ := victoria.QueryMetrics("memory_used_mb")
		memFree, _ := victoria.QueryMetrics("memory_free_mb")
		result["mem_used_mb"] = parseFirstValue(memUsed)
		result["mem_free_mb"] = parseFirstValue(memFree)

		// Top processes by CPU
		topCPU, _ := victoria.QueryMetrics("topk(5, process_cpu_pct)")
		result["top_cpu"] = parseProcessResults(topCPU)

		// Top processes by memory
		topMem, _ := victoria.QueryMetrics("topk(5, process_memory_mb)")
		result["top_mem"] = parseProcessResults(topMem)

		return result
	})

	w.Bind("askQuestion", func(query string) map[string]interface{} {
		return postJSON(serverURL+"/query", map[string]string{"query": query})
	})

	w.Bind("getRecommendations", func() map[string]interface{} {
		return getJSON(serverURL + "/recommend")
	})

	w.Bind("sendFeedback", func(id int64, val int) map[string]string {
		body := fmt.Sprintf(`{"interaction_id": %d, "feedback": %d}`, id, val)
		resp, err := http.Post(serverURL+"/feedback", "application/json", bytes.NewBufferString(body))
		if err != nil {
			return map[string]string{"status": "error: " + err.Error()}
		}
		defer resp.Body.Close()
		return map[string]string{"status": "ok"}
	})

	w.SetHtml(composedHTML)
	w.Run()
}

func parseFirstValue(result string) string {
	if result == "" {
		return "0"
	}
	lines := strings.SplitN(result, "\n", 2)
	parts := strings.SplitN(lines[0], ": ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return "0"
}

func parseFirstValueBytes(result string) string {
	val := parseFirstValue(result)
	var f float64
	fmt.Sscanf(val, "%f", &f)
	return fmt.Sprintf("%.0f", f/(1024*1024))
}

func parseProcessResults(result string) []map[string]string {
	if result == "" {
		return nil
	}
	var entries []map[string]string
	for _, line := range strings.Split(strings.TrimSpace(result), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		name := extractLabel(parts[0], "process_name")
		if name == "" {
			name = parts[0]
		}
		entries = append(entries, map[string]string{
			"name":  name,
			"value": strings.TrimSpace(parts[1]),
		})
	}
	return entries
}

func extractLabel(metric, label string) string {
	key := label + `="`
	idx := strings.Index(metric, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(metric[start:], `"`)
	if end < 0 {
		return ""
	}
	return metric[start : start+end]
}

func postJSON(url string, payload interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	body, err := json.Marshal(payload)
	if err != nil {
		result["error"] = "Serialization error: " + err.Error()
		return result
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		result["error"] = "Cannot reach server: " + err.Error()
		return result
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		result["error"] = "Read error: " + err.Error()
		return result
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse error from body if it's JSON
		var errData map[string]interface{}
		if json.Unmarshal(respBody, &errData) == nil {
			if errMsg, ok := errData["error"].(string); ok {
				result["error"] = errMsg
				return result
			}
		}
		result["error"] = fmt.Sprintf("Server returned status %d: %s", resp.StatusCode, string(respBody))
		return result
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		result["error"] = "Malformed JSON response: " + err.Error()
	}
	return result
}

func getJSON(url string) map[string]interface{} {
	result := map[string]interface{}{}
	resp, err := http.Get(url)
	if err != nil {
		result["error"] = "Cannot reach server: " + err.Error()
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result["error"] = "Read error: " + err.Error()
		return result
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		if json.Unmarshal(body, &errData) == nil {
			if errMsg, ok := errData["error"].(string); ok {
				result["error"] = errMsg
				return result
			}
		}
		result["error"] = fmt.Sprintf("Server returned status %d: %s", resp.StatusCode, string(body))
		return result
	}

	if err := json.Unmarshal(body, &result); err != nil {
		result["error"] = "Malformed JSON response: " + err.Error()
	}
	return result
}
