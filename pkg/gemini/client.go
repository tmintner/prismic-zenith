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

	// System instruction to act as a macOS system analyst
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("You are Zenith, an AI agent focused on macOS system analysis. " +
				"You have access to SQLite tables: 'system_logs', 'system_metrics', and 'process_metrics'. " +
				"Your goal is to translate natural language questions into SQL queries, " +
				"execute them, and explain the results. " +
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
	prompt := fmt.Sprintf("Based on the following user query, provide ONLY a SQLite SQL query to retrieve the relevant data. "+
		"Tables: \n"+
		"- system_logs (timestamp, process, subsystem, category, level, message)\n"+
		"- system_metrics (timestamp, cpu_usage_pct, memory_used_mb, memory_free_mb)\n\n"+
		"Query: %s\n\nSQL:", userQuery)

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
