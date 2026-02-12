package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"zenith/pkg/collector"
	"zenith/pkg/db"
	"zenith/pkg/gemini"
	"zenith/pkg/llm"
	"zenith/pkg/ollama"
)

type QueryRequest struct {
	Query string `json:"query"`
}

type QueryResponse struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

var DefaultAPIKey string

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
	collectInterval := flag.String("interval", "5m", "Collection interval (e.g., 5m, 1h)")

	envKey := os.Getenv("GEMINI_API_KEY")
	defaultKey := envKey
	if defaultKey == "" {
		defaultKey = DefaultAPIKey
	}

	provider := flag.String("provider", "gemini", "LLM Provider (gemini, ollama)")
	modelName := flag.String("model", "", "Model name for local provider (default: gemma2:2b)")
	apiKey := flag.String("key", defaultKey, "Gemini API Key")
	flag.Parse()

	if *provider == "gemini" && *apiKey == "" {
		log.Fatal("Gemini API key is required (via -key, GEMINI_API_KEY env, or embedded DefaultAPIKey)")
	}

	database := db.NewVictoriaDB("http://localhost:8428")
	log.Println("Using VictoriaMetrics at http://localhost:8428")

	// Initialize LLM Provider
	var llmProvider llm.Provider
	ctx := context.Background()

	switch *provider {
	case "gemini":
		client, err := gemini.NewClient(ctx, *apiKey)
		if err != nil {
			log.Fatalf("failed to create gemini client: %v", err)
		}
		llmProvider = client
		log.Println("Using Gemini Provider")
	case "ollama":
		model := *modelName
		if model == "" {
			model = "gemma2:2b"
		}
		llmProvider = ollama.NewClient("http://localhost:11434", model)
		log.Printf("Using Ollama Provider (Model: %s)", model)
	default:
		log.Fatalf("Unknown provider: %s", *provider)
	}

	// Start Background Collection
	go startScheduler(database, *collectInterval)

	// Start HTTP Server
	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		handleQuery(w, r, database, llmProvider)
	})

	log.Printf("Starting Zenith Server on port %d...", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

func startScheduler(database *db.VictoriaDB, intervalStr string) {
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Printf("Invalid interval format '%s', defaulting to 5m: %v", intervalStr, err)
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on startup
	log.Println("Running initial collection...")
	runCollection(database, intervalStr)

	for range ticker.C {
		log.Println("Running scheduled collection...")
		runCollection(database, intervalStr)
	}
}

func runCollection(database *db.VictoriaDB, duration string) {
	if err := collector.CollectLogs(database, duration); err != nil {
		log.Printf("Error collecting logs: %v", err)
	}
	if err := collector.CollectMetrics(database); err != nil {
		log.Printf("Error collecting metrics: %v", err)
	}
	if err := collector.CollectProcessMetrics(database); err != nil {
		log.Printf("Error collecting process metrics: %v", err)
	}
	log.Println("Finished collection.")
}

func handleQuery(w http.ResponseWriter, r *http.Request, database *db.VictoriaDB, client llm.Provider) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Analyzing query: %s", req.Query)

	var sqlQuery string
	var results string
	var err error

	// Retry loop for SQL generation and execution (up to 3 attempts)
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		sqlQuery, err = client.GenerateSQL(req.Query)
		if err != nil {
			log.Printf("Attempt %d: Failed to generate MetricsQL: %v", attempt, err)
			if attempt == maxRetries {
				respondError(w, fmt.Sprintf("Failed to generate MetricsQL after %d attempts: %v", maxRetries, err))
				return
			}
			continue
		}

		log.Printf("Attempt %d: Executing Metrics Query: %s", attempt, sqlQuery)
		results, err = database.QueryMetrics(sqlQuery)
		if err != nil {
			log.Printf("Attempt %d: Query Execution Error: %v", attempt, err)
			if attempt == maxRetries {
				respondError(w, fmt.Sprintf("Failed to execute query after %d attempts: %v", maxRetries, err))
				return
			}
			continue
		}
		log.Printf("Attempt %d: Query Executed successfully.", attempt)
		// Success!
		break
	}

	explanation, err := client.ExplainResults(req.Query, sqlQuery, results)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to explain results: %v", err))
		return
	}

	log.Println("Query analysis finished.")
	respondJSON(w, QueryResponse{Answer: explanation})
}

func respondJSON(w http.ResponseWriter, resp interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	log.Println("Response sent to client.")
}

func respondError(w http.ResponseWriter, msg string) {
	log.Println("Error:", msg)
	respondJSON(w, QueryResponse{Error: msg})
}
