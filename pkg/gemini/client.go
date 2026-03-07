package gemini

import (
	"context"
	"fmt"

	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Client struct {
	Ctx    context.Context
	Model  *genai.GenerativeModel
	Client *genai.Client
}

func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}

	model := client.GenerativeModel("gemini-3-flash-preview")

	// System instruction to act as a system analyst
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("You are Zenith, an AI agent focused on system analysis. " +
				"You have access to two databases:\n" +
				"1. VictoriaMetrics (Metrics): Use this for numerical data over time (CPU, RAM, Disk I/O, Network). " +
				"Metrics: 'cpu_usage_pct', 'memory_used_mb', 'process_cpu_pct', 'process_memory_mb', " +
				"'srum_network_bytes_sent_total', 'srum_network_bytes_received_total', 'srum_app_cycle_time_total', 'srum_app_bytes_read_total', 'srum_app_bytes_written_total'. " +
				"Query this using MetricsQL (PromQL-compatible).\n" +
				"2. VictoriaLogs (Logs): Use this for event logs (Windows Event Log, console messages). " +
				"Query using LogsQL (Syntax: `field:value` or `field:\"value\"`). Fields: 'processName', 'subsystem', 'category', 'messageType', 'eventMessage'. " +
				"NEVER use square brackets `[]`, NEVER use comparison operators like `>`, `<`, `>=`, `<=`, and NEVER use time filters (e.g., `timestamp`, `now`, `-1d`) in LogsQL filters. All time filtering is handled by the server.\n\n" +
				"Your goal is to translate natural language questions into EXACTLY ONE appropriate query, " +
				"prefixed with either 'METRIC:' or 'LOG:'. " +
				"Do NOT return multiple lines or multiple queries. " +
				"Be extremely concise, focus on the data, and avoid conversational filler."),
		},
	}

	return &Client{
		Ctx:    ctx,
		Model:  model,
		Client: client,
	}, nil
}

func (c *Client) GenerateSQL(userQuery string) (string, error) {
	prompt := fmt.Sprintf("Based on the following user query, provide ONLY ONE database query prefixed with 'METRIC:' or 'LOG:'.\n\n"+
		"Metrics (VictoriaMetrics - MetricsQL):\n"+
		"- System-wide (NO label filter needed): cpu_usage_pct, memory_used_mb\n"+
		"- Per-process (use label `process_name`): process_cpu_pct, process_memory_mb\n"+
		"- SRUM app (use labels `app_name`, `user_name`): srum_app_cycle_time_total, srum_app_bytes_read_total, srum_app_bytes_written_total, srum_app_duration_ms, srum_app_foreground_cycle_time_total, srum_app_background_cycle_time_total\n"+
		"- SRUM network (NO label needed): srum_network_bytes_sent_total, srum_network_bytes_received_total\n\n"+
		"Logs (VictoriaLogs - LogsQL):\n"+
		"- Fields: processName, subsystem, category, messageType, eventMessage\n"+
		"- Syntax: `field:value` or `field:\"exact string\"`\n\n"+
		"Rules:\n"+
		"1. Return ONLY ONE line. Do NOT truncate metric names.\n"+
		"2. NEVER add a label filter unless the user asks about a specific app or process.\n"+
		"3. NEVER use placeholder label values like 'your_process_name'. Omit the label entirely.\n"+
		"4. NEVER combine metrics and logs in the same query. Choose ONE.\n"+
		"5. SRUM data is exclusively METRICS, never LOGS.\n"+
		"6. NEVER compare metrics to strings. To check for existence, use `metric_name > 0`.\n"+
		"7. MetricsQL uses lowercase logical operators: `and`, `or`, `unless`.\n"+
		"8. MetricsQL NEVER uses SQL syntax like `ORDER BY` or `LIMIT`. To rank results, use `topk(n, metric)`.\n"+
		"9. LogsQL uses `:` for equality (NEVER `=` or `==`).\n"+
		"10. LogsQL NEVER uses comparison operators like `>`, `<`, `>=`, `<=`. Use `:` for all filters.\n"+
		"11. LogsQL NEVER uses time-related keywords in the query string (e.g., `timestamp`, `@timestamp`, `now`, `24h`, `1d`).\n"+
		"12. NEVER use square brackets `[]` for filters or grouping in LogsQL.\n"+
		"13. For arithmetic, do NOT repeat the prefix.\n\n"+
		"Example 'System performance': `METRIC:avg(cpu_usage_pct)`\n"+
		"Example 'Memory': `METRIC:avg(memory_used_mb)`\n"+
		"Example 'Process CPU': `METRIC:topk(5, process_cpu_pct)`\n"+
		"Example 'Any SRUM data': `METRIC:srum_app_bytes_read_total > 0`\n"+
		"Example 'Most disk IO apps': `METRIC:topk(10, srum_app_bytes_written_total)`\n"+
		"Example 'Most CPU apps (SRUM)': `METRIC:topk(10, srum_app_cycle_time_total)`\n"+
		"Example LogsQL: `LOG:eventMessage:\"error\" AND processName:\"wifid\"`\n\n"+
		"Query: %s\n\nResponse:", userQuery)

	resp, err := c.Model.GenerateContent(c.Ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	sql := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			sql += string(text)
		}
	}

	return cleanSQL(sql), nil
}

func cleanSQL(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```sql")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// If multiple lines, pick the first one that starts with METRIC or LOG
	// or the first non-empty line.
	var selected string
	lines := strings.Split(s, "\n")
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

	// Fallback to first non-empty line
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
	// to handle hallucinations like "METRIC:m1 + METRIC:m2"
	upperSelected := strings.ToUpper(selected)
	hasLog := strings.HasPrefix(upperSelected, "LOG:")

	res := selected
	// Case-insensitive removal of all prefixes
	reMetric := strings.NewReplacer("METRIC:", "", "metric:", "", "Metric:", "")
	reLog := strings.NewReplacer("LOG:", "", "log:", "", "Log:", "")
	res = reMetric.Replace(res)
	res = reLog.Replace(res)
	res = strings.TrimSpace(res)

	// 4. Strip any leading/trailing square brackets hallucinated by the LLM
	res = strings.TrimSpace(res)
	if strings.HasPrefix(res, "[") && strings.HasSuffix(res, "]") {
		res = res[1 : len(res)-1]
	}
	res = strings.TrimSpace(res)

	// 5. Strip any hallucinated time filters (e.g., AND timestamp > now - 24h)
	if hasLog {
		timeFilters := []string{
			"AND timestamp", "AND @timestamp", "AND _time",
			"timestamp:", "@timestamp:", "_time:",
		}
		for _, tf := range timeFilters {
			if idx := strings.Index(strings.ToUpper(res), tf); idx != -1 {
				res = res[:idx]
				break
			}
		}
		// Also catch trailing comparisons if the word "timestamp" was missed
		if idx := strings.Index(res, " > "); idx != -1 {
			res = res[:idx]
		}
	}
	res = strings.TrimSpace(res)

	if hasLog {
		return "LOG:" + res
	}
	return "METRIC:" + res
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	prompt := fmt.Sprintf("Analyze the database results below to answer the user's question.\n\n"+
		"Rules:\n"+
		"1. If the results are 'NO_DATA_FOUND' or empty, say 'No data found for this query'.\n"+
		"2. If results contain metrics with value 0, explain that those apps/processes showed no activity for that metric - do NOT say 'no data found'.\n"+
		"3. Do NOT invent application names, process IDs, or numerical values.\n"+
		"4. Do NOT use placeholder names like 'Application X' or 'Process 123'.\n"+
		"5. Be extremely concise. If all values are 0, say so clearly.\n\n"+
		"User Query: %s\n"+
		"SQL/Query Executed: %s\n"+
		"Database Results: %s\n\n"+
		"Explanation:", userQuery, sql, results)

	resp, err := c.Model.GenerateContent(c.Ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	explanation := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			explanation += string(text)
		}
	}

	return explanation, nil
}

func (c *Client) GenerateRecommendations(systemData string) (string, error) {
	prompt := fmt.Sprintf("You are Zenith, an AI expert in system performance.\n"+
		"Based on the following recent system data, provide 3-5 concrete recommendations for performance improvement.\n"+
		"Be extremely concise, focus on actionable advice, and avoid conversational filler.\n\n"+
		"System Data:\n%s\n\nRecommendations:", systemData)

	resp, err := c.Model.GenerateContent(c.Ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	recommendations := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			recommendations += string(text)
		}
	}

	return recommendations, nil
}
