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
				"1. VictoriaMetrics (Metrics): 'cpu_usage_pct', 'memory_used_mb', 'memory_free_mb', 'process_cpu_pct', 'process_memory_mb', " +
				"'srum_network_bytes_sent_total', 'srum_network_bytes_received_total', 'srum_app_cycle_time_total', 'srum_app_bytes_read_total', 'srum_app_bytes_written_total'. " +
				"Query this using MetricsQL (PromQL-compatible).\n" +
				"2. VictoriaLogs (Logs): Contains system logs with fields like 'processName', 'subsystem', 'category', 'messageType', 'eventMessage'. " +
				"Query this using LogsQL.\n\n" +
				"Your goal is to translate natural language questions into the appropriate query, " +
				"prefixed with either 'METRIC:' or 'LOG:'. " +
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
	prompt := fmt.Sprintf("Based on the following user query, provide ONLY the database query prefixed with 'METRIC:' or 'LOG:'.\n\n"+
		"Metrics (VictoriaMetrics - MetricsQL):\n"+
		"- cpu_usage_pct (labels: host)\n"+
		"- memory_used_mb (labels: host)\n"+
		"- memory_free_mb (labels: host)\n"+
		"- process_cpu_pct (labels: pid, process_name)\n"+
		"- process_memory_mb (labels: pid, process_name)\n"+
		"- srum_network_bytes_sent_total (labels: interface_luid)\n"+
		"- srum_network_bytes_received_total (labels: interface_luid)\n"+
		"- srum_app_cycle_time_total (labels: app_name)\n"+
		"- srum_app_bytes_read_total (labels: app_name)\n"+
		"- srum_app_bytes_written_total (labels: app_name)\n\n"+
		"Logs (VictoriaLogs - LogsQL):\n"+
		"- Available fields: processName, subsystem, category, messageType, eventMessage\n"+
		"- Example LogsQL: `eventMessage:\"error\"`, `processName:\"wifid\"` \n\n"+
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
	return strings.TrimSpace(s)
}

func (c *Client) ExplainResults(userQuery, sql, results string) (string, error) {
	prompt := fmt.Sprintf("The user asked: %s\n"+
		"The SQL executed was: %s\n"+
		"The results from the database are: %s\n\n"+
		"Explain these results briefly. Be extremely concise, focus on the data, and avoid conversational filler.", userQuery, sql, results)

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
