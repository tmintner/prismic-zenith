package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LogEntry struct {
	Timestamp    string `json:"timestamp"`
	ProcessID    int    `json:"processID"`
	ProcessName  string `json:"processName"`
	Subsystem    string `json:"subsystem"`
	Category     string `json:"category"`
	LogLevel     string `json:"messageType"`
	EventMessage string `json:"eventMessage"`
}

type VictoriaDB struct {
	MetricsURL string
	LogsURL    string
	Client     *http.Client
}

func NewVictoriaDB(metricsURL, logsURL string) *VictoriaDB {
	return &VictoriaDB{
		MetricsURL: metricsURL,
		LogsURL:    logsURL,
		Client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *VictoriaDB) InsertMetric(name string, value float64, labels map[string]string) error {
	// Use Prometheus exposition format via /api/v1/import/prometheus.
	// This stores the metric with exactly the name given, no suffix or doubling.
	// Format: metric_name{label1="val1",label2="val2"} value timestamp_ms

	var labelParts []string
	for k, val := range labels {
		// Escape backslashes and double-quotes inside label values
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(val)
		labelParts = append(labelParts, fmt.Sprintf(`%s="%s"`, k, escaped))
	}

	var line string
	if len(labelParts) > 0 {
		line = fmt.Sprintf("%s{%s} %f %d\n", name, strings.Join(labelParts, ","), value, time.Now().UnixMilli())
	} else {
		line = fmt.Sprintf("%s %f %d\n", name, value, time.Now().UnixMilli())
	}

	resp, err := v.Client.Post(v.MetricsURL+"/api/v1/import/prometheus", "text/plain", bytes.NewBufferString(line))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victoria metrics write failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func (v *VictoriaDB) QueryMetrics(query string) (string, error) {
	u, err := url.Parse(v.MetricsURL + "/api/v1/query")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("query", query)
	// step=600 extends the lookback window to 10 minutes so metrics written
	// every 5 minutes (e.g. SRUM) are always found between collection cycles.
	q.Set("step", "600")
	u.RawQuery = q.Encode()

	resp, err := v.Client.Get(u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("victoria metrics query failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Format results in a clean, LLM-readable way
	var out bytes.Buffer
	for _, res := range result.Data.Result {
		// Extract the numeric value (index 1 of the [timestamp, value] pair)
		val := ""
		if len(res.Value) >= 2 {
			val = fmt.Sprintf("%v", res.Value[1])
		}

		// Build a label description; omit __name__ since we print query context elsewhere
		var labelParts []string
		for k, v := range res.Metric {
			if k != "__name__" {
				labelParts = append(labelParts, fmt.Sprintf("%s=%q", k, v))
			}
		}
		name := res.Metric["__name__"]
		if name == "" {
			name = "result"
		}
		if len(labelParts) > 0 {
			fmt.Fprintf(&out, "%s{%s}: %s\n", name, strings.Join(labelParts, ", "), val)
		} else {
			fmt.Fprintf(&out, "%s: %s\n", name, val)
		}
	}

	return out.String(), nil
}

// InsertLog inserts a log entry into VictoriaLogs.
func (v *VictoriaDB) InsertLog(entry interface{}) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// VictoriaLogs endpoint for JSON line insertion
	resp, err := v.Client.Post(v.LogsURL+"/insert/jsonline", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victoria logs write failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// InsertLogs inserts multiple log entries into VictoriaLogs in a single batch.
func (v *VictoriaDB) InsertLogs(entries []LogEntry) error {
	var buf bytes.Buffer
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	if buf.Len() == 0 {
		return nil
	}

	resp, err := v.Client.Post(v.LogsURL+"/insert/jsonline", "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victoria logs batch write failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func (v *VictoriaDB) QueryLogs(query string) (string, error) {
	u, err := url.Parse(v.LogsURL + "/select/logsql/query")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	resp, err := v.Client.Get(u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("victoria logs query failed (%d): %s", resp.StatusCode, string(body))
	}

	// VictoriaLogs returns NDJSON. We'll read it line by line and format for LLM.
	var out bytes.Buffer
	decoder := json.NewDecoder(resp.Body)
	for {
		var logEntry map[string]interface{}
		if err := decoder.Decode(&logEntry); err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}

		// Format entry for LLM context
		entryStr, _ := json.Marshal(logEntry)
		out.Write(entryStr)
		out.WriteByte('\n')
	}

	return out.String(), nil
}
