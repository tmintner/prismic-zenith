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
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "phi4-mini" // Default model
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
		"1. VictoriaMetrics (Metrics): Query using MetricsQL (PromQL-compatible). Metrics: 'cpu_usage_pct', 'memory_used_mb', 'memory_free_mb', 'process_cpu_pct', 'process_memory_mb', 'srum_network_bytes_sent_total', 'srum_network_bytes_received_total', 'srum_app_cycle_time_total', 'srum_app_bytes_read_total', 'srum_app_bytes_written_total'.\n"+
		"2. VictoriaLogs (Logs): Query using LogsQL. Fields: processName, subsystem, category, messageType, eventMessage.\n\n"+
		"Based on the user query, provide ONLY the database query prefixed with 'METRIC:' or 'LOG:'. Do NOT include explanation or markdown.\n\n"+
		"Rules for Process Names:\n"+
		"- Process names can be unpredictable (e.g., 'Ollama' vs 'ollama').\n"+
		"- ALWAYS use case-insensitive regex for process names: `process_memory_mb{process_name=~\"(?i)ollama\"}`.\n\n"+
		"Example MetricsQL: `avg(cpu_usage_pct)`, `max(process_memory_mb{process_name=~\"(?i)ollama\"})` \n"+
		"Example LogsQL: `eventMessage:\"error\"`, `processName:\"wifid\"` \n\n"+
		"Query: %s\n\n"+
		"Response:", userQuery)

	resp, err := c.generate(prompt)
	if err != nil {
		return "", err
	}

	return cleanSQL(resp), nil
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	prompt := fmt.Sprintf("System: You are Zenith, an AI expert in macOS system performance. "+
		"Analyze the database results below to answer the user's question. "+
		"Be extremely concise, focus on data insights, and avoid conversational filler. "+
		"Do NOT explain the SQL query syntax. "+
		"If the results are empty, say 'No relevant data found'.\n\n"+
		"User Query: %s\n"+
		"SQL Executed: %s\n"+
		"Database Results: %s\n\n"+
		"Analysis:", userQuery, sql, results)

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
			// If tag is open but not closed, strip from start
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

	// 3. Remove trailing commas before FROM (common model mistake)
	// This is a simple regex-like fix: replace ", FROM" (case insensitive) with " FROM"
	s = strings.ReplaceAll(s, ",\nFROM", "\nFROM")
	s = strings.ReplaceAll(s, ", FROM", " FROM")
	s = strings.ReplaceAll(s, ",\nfrom", "\nfrom")
	s = strings.ReplaceAll(s, ", from", " from")

	// 4. If the model wrapped it in triple backticks, extract it
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			lowered := strings.ToLower(trimmed)
			if strings.HasPrefix(lowered, "sql") {
				return strings.TrimSpace(trimmed[3:])
			}
			// If it contains keywords but no lang tag
			if len(trimmed) > 0 && (strings.Contains(lowered, "select") || strings.Contains(lowered, "insert") || strings.Contains(lowered, "update")) {
				return trimmed
			}
		}
	}

	// 5. Fallback: Check if there's any text before the first SELECT
	lowered := strings.ToLower(s)
	if idx := strings.Index(lowered, "select"); idx != -1 {
		// Try to see if it's the start of a statement
		return strings.TrimSpace(s[idx:])
	}

	// Final Fallback: strip common prefixes
	s = strings.TrimPrefix(s, "SQL:")
	s = strings.TrimPrefix(s, "Sql:")
	s = strings.TrimPrefix(s, "sql:")
	return strings.TrimSpace(s)
}
