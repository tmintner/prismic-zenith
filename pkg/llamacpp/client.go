package llamacpp

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
	Client  *http.Client
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 300 * time.Second},
	}
}

func (c *Client) generate(prompt string, systemPrompt string) (string, error) {
	messages := []ChatMessage{}
	if systemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, ChatMessage{Role: "user", Content: prompt})

	reqBody := ChatRequest{
		Messages: messages,
		Stream:   false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := c.Client.Post(c.BaseURL+"/v1/chat/completions", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("failed to connect to llama.cpp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llama.cpp API error: %s", string(body))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", fmt.Errorf("llama.cpp error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func (c *Client) GenerateSQL(userQuery string) (string, error) {
	systemPrompt := "You are Zenith, an AI expert in system performance. " +
		"You have access to two databases:\n" +
		"1. VictoriaMetrics (Metrics): Query using MetricsQL (PromQL-compatible). Metrics: 'cpu_usage_pct', 'memory_used_mb', 'process_cpu_pct', 'process_memory_mb', 'srum_network_bytes_sent_total', 'srum_network_bytes_received_total', 'srum_app_cycle_time_total', 'srum_app_bytes_read_total', 'srum_app_bytes_written_total'.\n" +
		"2. VictoriaLogs (Logs): Query using LogsQL (Syntax: `field:value`). Fields: processName, subsystem, category, messageType, eventMessage.\n\n" +
		"Based on the user query, provide EXACTLY ONE database query prefixed with 'METRIC:' or 'LOG:'. Do NOT include explanation or markdown.\n\n" +
		"Rules for Queries:\n" +
		"- Return ONLY ONE line. Multi-line responses will fail.\n" +
		"- NEVER combine metrics and logs in the same query. Choose ONE.\n" +
		"- SRUM data (network, disk, cycle time) is exclusively stored as METRICS, never as LOGS.\n" +
		"- For SRUM app metrics, use the label `app_name`.\n" +
		"- For process metrics, use the label `process_name`.\n" +
		"- MetricsQL regex uses `=~`, e.g., `process_memory_mb{process_name=~\"(?i)ollama\"}`.\n" +
		"- LogsQL uses `:` for equality, NEVER `=`, `==`, or `~` (e.g. `processName:\"wifid\"`).\n" +
		"- LogsQL uses `AND`/`OR` for logic, NEVER `,` or `|`.\n" +
		"- For arithmetic, do NOT repeat the prefix, e.g., `METRIC:sum(m1) + sum(m2)`.\n\n" +
		"Example MetricsQL: `avg(cpu_usage_pct)`, `srum_network_bytes_sent_total > 0`\n" +
		"Example LogsQL: `eventMessage:\"error\" AND processName:\"wifid\"`"

	prompt := fmt.Sprintf("Query: %s\n\nResponse:", userQuery)

	resp, err := c.generate(prompt, systemPrompt)
	if err != nil {
		return "", err
	}

	return cleanSQL(resp), nil
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	systemPrompt := "You are Zenith, an AI expert in system performance. " +
		"Analyze the database results below to answer the user's question. " +
		"Rules:\n" +
		"1. If the results are 'NO_DATA_FOUND' or empty, you MUST say 'No data found for this query'.\n" +
		"2. Do NOT invent names, PIDs, or values.\n" +
		"3. Do NOT use placeholders like 'Application X'.\n" +
		"4. Be extremely concise."

	prompt := fmt.Sprintf("User Query: %s\nSQL Executed: %s\nDatabase Results: %s\n\nAnalysis:", userQuery, sql, results)

	return c.generate(prompt, systemPrompt)
}

func (c *Client) GenerateRecommendations(systemData string) (string, error) {
	systemPrompt := "You are Zenith, an AI expert in system performance. " +
		"Based on the following recent system data, provide 3-5 concrete recommendations for performance improvement. " +
		"Be extremely concise, focus on actionable advice, and avoid conversational filler."

	prompt := fmt.Sprintf("System Data:\n%s\n\nRecommendations:", systemData)

	return c.generate(prompt, systemPrompt)
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
