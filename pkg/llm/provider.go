package llm

// Provider defines the interface for an LLM provider (e.g. Gemini, Ollama).
type Provider interface {
	// GenerateSQL translates a natural language query into a SQL query for the zenith.db.
	GenerateSQL(userQuery string) (string, error)

	// ExplainResults explains the results of a SQL query in natural language.
	ExplainResults(userQuery, sql, results string) (string, error)

	// GenerateRecommendations analyzes recent system data and provides performance improvement recommendations.
	GenerateRecommendations(systemData string) (string, error)
}
