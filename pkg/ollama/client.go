package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type GenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

func NewClient(baseURL, model string) *Client {
	if model == "" {
		model = "qwen2.5-coder:7b" // Default model
	}

	return &Client{
		BaseURL: baseURL,
		Model:   model,
		Client:  &http.Client{Timeout: 300 * time.Second},
	}
}

func (c *Client) generate(prompt string) (string, error) {
	reqBody := GenerateRequest{
		Model:  c.Model,
		Prompt: prompt,
		Stream: false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := c.Client.Post(c.BaseURL+"/api/generate", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("failed to connect to Ollama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama API error: %s", string(body))
	}

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", err
	}

	if genResp.Error != "" {
		return "", fmt.Errorf("ollama error: %s", genResp.Error)
	}

	return genResp.Response, nil
}

func (c *Client) GenerateSQL(userQuery string) (string, error) {
	prompt := fmt.Sprintf("You are Zenith, an AI expert in system performance. "+
		"You have access to two databases:\n"+
		"1. VictoriaMetrics (Metrics): Query using MetricsQL (PromQL-compatible).\n"+
		"   System-wide (NO label filter needed): cpu_usage_pct, memory_used_mb\n"+
		"   Per-process (use label `process_name`): process_cpu_pct, process_memory_mb\n"+
		"   SRUM app (use labels `app_name`, `user_name`): srum_app_cycle_time_total, srum_app_bytes_read_total, srum_app_bytes_written_total, srum_app_duration_ms, srum_app_foreground_cycle_time_total, srum_app_background_cycle_time_total\n"+
		"   SRUM network (NO label needed): srum_network_bytes_sent_total, srum_network_bytes_received_total\n"+
		"2. VictoriaLogs (Logs): Query using LogsQL (Syntax: `field:value`). Fields: processName, subsystem, category, messageType, eventMessage.\n\n"+
		"Based on the user query, provide EXACTLY ONE database query prefixed with 'METRIC:' or 'LOG:'. Do NOT include explanation or markdown.\n\n"+
		"Rules for Queries:\n"+
		"- Return ONLY ONE line. Do NOT truncate the query or cut off metric names.\n"+
		"- NEVER add a label filter unless the user asks about a specific app or process.\n"+
		"- NEVER use placeholder label values like 'your_process_name'. Omit the label entirely.\n"+
		"- NEVER combine metrics and logs in the same query. Choose ONE.\n"+
		"- SRUM data is exclusively stored as METRICS, never LOGS.\n"+
		"- NEVER compare metrics to strings. To check for existence, just use `metric_name > 0`.\n"+
		"- MetricsQL regex uses `=~`, e.g., `process_cpu_pct{process_name=~\"(?i)ollama\"}`.\n"+
		"- MetricsQL uses lowercase logical operators: `and`, `or`, `unless`.\n"+
		"- LogsQL uses `:` for equality, NEVER `=` or `==`.\n"+
		"- LogsQL uses uppercase logical operators: `AND`, `OR`.\n"+
		"- NEVER use square brackets `[]` for filters or grouping in LogsQL.\n\n"+
		"Example 'System performance': `METRIC:avg(cpu_usage_pct)`\n"+
		"Example 'Memory': `METRIC:avg(memory_used_mb)`\n"+
		"Example 'Process CPU': `METRIC:topk(5, process_cpu_pct)`\n"+
		"Example 'Any SRUM data': `METRIC:srum_app_bytes_read_total > 0`\n"+
		"Example 'Most disk IO apps': `METRIC:topk(10, srum_app_bytes_written_total)`\n"+
		"Example 'Most CPU apps (SRUM)': `METRIC:topk(10, srum_app_cycle_time_total)`\n"+
		"Example LogsQL: `LOG:eventMessage:\"error\" AND processName:\"wifid\"`\n\n"+
		"Query: %s\n\n"+
		"Response:", userQuery)

	resp, err := c.generate(prompt)
	if err != nil {
		return "", err
	}

	return cleanSQL(resp), nil
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	prompt := fmt.Sprintf("System: You are Zenith, an AI expert in system performance. "+
		"Analyze the database results below to answer the user's question. "+
		"Rules:\n"+
		"1. If the results are 'NO_DATA_FOUND' or empty, say 'No data found for this query'.\n"+
		"2. If results contain metrics with value 0, explain that those apps/processes showed no activity for that metric - do NOT say 'no data found'.\n"+
		"3. Do NOT invent names, PIDs, or values.\n"+
		"4. Do NOT use placeholders like 'Application X'.\n"+
		"5. Be extremely concise. If all values are 0, say so clearly.\n\n"+
		"User Query: %s\n"+
		"SQL Executed: %s\n"+
		"Database Results: %s\n\n"+
		"Analysis:", userQuery, sql, results)

	return c.generate(prompt)
}

func (c *Client) GenerateRecommendations(systemData string) (string, error) {
	prompt := fmt.Sprintf("System: You are Zenith, an AI expert in system performance. "+
		"Based on the following recent system data, provide 3-5 concrete recommendations for performance improvement. "+
		"Be extremely concise, focus on actionable advice, and avoid conversational filler.\n\n"+
		"System Data:\n%s\n\nRecommendations:", systemData)

	return c.generate(prompt)
}

func cleanSQL(s string) string {
	s = strings.TrimSpace(s)

	// 1. Remove <think>...</think> blocks if present
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+8:]
		s = strings.TrimSpace(s)
	}

	// 2. Strip SQL line comments (-- ...)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx != -1 {
			lines[i] = strings.TrimSpace(line[:idx])
		}
	}
	s = strings.Join(lines, "\n")
	s = strings.TrimSpace(s)

	// 3. Extract the best line
	var selected string
	lines = strings.Split(s, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "METRIC:") || strings.HasPrefix(upper, "LOG:") {
			selected = trimmed
			break
		}
	}

	// Fallback: pick the first non-empty line
	if selected == "" {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				selected = trimmed
				break
			}
		}
	}

	if selected == "" {
		return s
	}

	// Globally remove all instances of METRIC: and LOG: from the selected line
	upperSelected := strings.ToUpper(selected)
	hasLog := strings.HasPrefix(upperSelected, "LOG:")

	res := selected
	reMetric := strings.NewReplacer("METRIC:", "", "metric:", "", "Metric:", "")
	reLog := strings.NewReplacer("LOG:", "", "log:", "", "Log:", "")
	res = reMetric.Replace(res)
	res = reLog.Replace(res)
	res = strings.TrimSpace(res)

	if hasLog {
		return "LOG:" + res
	}
	return "METRIC:" + res
}
