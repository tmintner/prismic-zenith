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
				"Query using LogsQL (Syntax: `field:value` or `field:\"value\"`). Fields: 'processName', 'subsystem', 'category', 'messageType', 'eventMessage'.\n\n" +
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
		"- cpu_usage_pct (labels: host)\n"+
		"- memory_used_mb (labels: host)\n"+
		"- process_cpu_pct (labels: pid, process_name)\n"+
		"- process_memory_mb (labels: pid, process_name)\n"+
		"- srum_network_bytes_sent_total (labels: interface)\n"+
		"- srum_network_bytes_received_total (labels: interface)\n"+
		"- srum_app_cycle_time_total (labels: app_name)\n"+
		"- srum_app_bytes_read_total (labels: app_name)\n"+
		"- srum_app_bytes_written_total (labels: app_name)\n\n"+
		"Logs (VictoriaLogs - LogsQL):\n"+
		"- Fields: processName, subsystem, category, messageType, eventMessage\n"+
		"- Syntaxes: `field:value`, `field:\"exact string\"`, `field:~\"regex\"` \n"+
		"- Example LogsQL: `eventMessage:\"error\"`, `processName:\"wifid\"` \n\n"+
		"Rules:\n"+
		"1. Return ONLY ONE line. Do NOT truncate the query or cut off metric names (e.g. output `srum_network_bytes_sent_total`, NOT `srum_net`).\n"+
		"2. NEVER combine metrics and logs in the same query. Choose ONE.\n"+
		"3. SRUM data (network, disk, cycle time) is exclusively stored as METRICS, never as LOGS.\n"+
		"4. NEVER compare metrics to strings (e.g. `metric == \"\"`). To check for existence, simply query the metric name (e.g., `srum_app_bytes_read_total > 0`).\n"+
		"5. For SRUM app metrics, use the label `app_name`.\n"+
		"6. MetricsQL uses lowercase logical operators: `and`, `or`, `unless` (NEVER `AND`/`OR`).\n"+
		"7. LogsQL uses `:` for equality, NEVER `=`, `==`, or `~` (e.g. `processName:\"wifid\"`).\n"+
		"8. LogsQL uses uppercase logical operators: `AND`, `OR`.\n"+
		"9. For arithmetic, do NOT repeat the prefix, e.g., `METRIC:sum(m1) + sum(m2)`.\n"+
		"Example MetricsQL: `METRIC:srum_network_bytes_sent_total > 0 or srum_network_bytes_received_total > 0`\n"+
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

	if hasLog {
		return "LOG:" + res
	}
	return "METRIC:" + res
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	prompt := fmt.Sprintf("Analyze the database results below to answer the user's question.\n\n"+
		"Rules:\n"+
		"1. If the results are 'NO_DATA_FOUND' or empty, you MUST say 'No data found for this query'.\n"+
		"2. Do NOT invent application names, process IDs, or numerical values.\n"+
		"3. Do NOT use placeholder names like 'Application X' or 'Process 123'.\n"+
		"4. Be extremely concise and focus ONLY on the data provided.\n\n"+
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
