package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	// Use Influx line protocol format for simplicity
	// measurement,tag1=val1,tag2=val2 value=123.45 timestamp
	line := fmt.Sprintf("%s", name)
	for k, val := range labels {
		line += fmt.Sprintf(",%s=%s", k, val)
	}
	line += fmt.Sprintf(" value=%f %d\n", value, time.Now().UnixNano())

	resp, err := v.Client.Post(v.MetricsURL+"/write", "text/plain", bytes.NewBufferString(line))
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

	// Simple serialization for LLM
	var out bytes.Buffer
	for _, res := range result.Data.Result {
		fmt.Fprintf(&out, "Metric: %v, Value: %v\n", res.Metric, res.Value)
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
