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

type VictoriaDB struct {
	MetricsURL string
	Client     *http.Client
}

func NewVictoriaDB(metricsURL string) *VictoriaDB {
	return &VictoriaDB{
		MetricsURL: metricsURL,
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

// TODO: VictoriaLogs implementation if available
func (v *VictoriaDB) InsertLog(entry interface{}) error {
	// For now, we might skip or use a simple metrics-based log counter if VictoriaLogs is missing
	return nil
}

func (v *VictoriaDB) QueryLogs(query string) (string, error) {
	return "VictoriaLogs is not yet configured.", nil
}
