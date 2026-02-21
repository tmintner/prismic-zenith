package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"zenith/pkg/collector"
	"zenith/pkg/config"
	"zenith/pkg/db"
	"zenith/pkg/gemini"
	"zenith/pkg/llm"
	"zenith/pkg/ollama"
	"zenith/pkg/rl"
)

type QueryRequest struct {
	Query string `json:"query"`
}

type QueryResponse struct {
	InteractionID int64  `json:"interaction_id,omitempty"`
	Answer        string `json:"answer"`
	Error         string `json:"error,omitempty"`
}

var DefaultAPIKey string

func main() {
	// Load config first
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	port := flag.Int("port", cfg.ServerPort, "HTTP server port")
	collectInterval := flag.String("interval", cfg.CollectInterval, "Collection interval (e.g., 5m, 1h)")
	metricsURL := flag.String("metrics-url", fmt.Sprintf("http://localhost:%d", cfg.MetricsPort), "VictoriaMetrics URL")
	logsURL := flag.String("logs-url", fmt.Sprintf("http://localhost:%d", cfg.LogsPort), "VictoriaLogs URL")

	// Default paths based on OS
	defaultMetricsBin := cfg.MetricsBin
	defaultLogsBin := cfg.LogsBin
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(defaultMetricsBin, ".exe") {
			defaultMetricsBin = "victoria-metrics.exe"
		}
		if !strings.HasSuffix(defaultLogsBin, ".exe") {
			defaultLogsBin = "victoria-logs.exe"
		}
	}

	metricsBin := flag.String("metrics-bin", defaultMetricsBin, "Path to victoria-metrics binary")
	logsBin := flag.String("logs-bin", defaultLogsBin, "Path to victoria-logs binary")
	metricsData := flag.String("metrics-data", cfg.MetricsData, "Path to VictoriaMetrics data")
	logsData := flag.String("logs-data", cfg.LogsData, "Path to VictoriaLogs data")

	envKey := os.Getenv("GEMINI_API_KEY")
	defaultKey := envKey
	if defaultKey == "" {
		defaultKey = cfg.GeminiAPIKey
		if defaultKey == "" {
			defaultKey = DefaultAPIKey
		}
	}

	provider := flag.String("provider", cfg.LLMProvider, "LLM Provider (gemini, ollama)")
	modelName := flag.String("model", cfg.OllamaModel, "Model name for local provider")
	apiKey := flag.String("key", defaultKey, "Gemini API Key")
	flag.Parse()

	if *provider == "gemini" && *apiKey == "" {
		log.Fatal("Gemini API key is required")
	}

	// Extract ports from URLs to start databases on the correct ports
	metricsPort := extractPort(*metricsURL, cfg.MetricsPort)
	logsPort := extractPort(*logsURL, cfg.LogsPort)

	// Start VictoriaMetrics and VictoriaLogs
	metricsCmd := startProcess(*metricsBin, "-storageDataPath", *metricsData, "-httpListenAddr", fmt.Sprintf(":%d", metricsPort))
	defer stopProcess(metricsCmd)

	logsCmd := startProcess(*logsBin, "-storageDataPath", *logsData, "-httpListenAddr", fmt.Sprintf(":%d", logsPort))
	defer stopProcess(logsCmd)

	// Wait a moment for databases to start
	time.Sleep(2 * time.Second)

	database := db.NewVictoriaDB(*metricsURL, *logsURL)
	log.Printf("Using VictoriaMetrics at %s", *metricsURL)
	log.Printf("Using VictoriaLogs at %s", *logsURL)

	// Initialize LLM Provider
	var llmProvider llm.Provider
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch *provider {
	case "gemini":
		client, err := gemini.NewClient(ctx, *apiKey)
		if err != nil {
			log.Fatalf("failed to create gemini client: %v", err)
		}
		llmProvider = client
		log.Println("Using Gemini Provider")
	case "ollama":
		ollamaURL := fmt.Sprintf("http://localhost:%d", cfg.OllamaPort)
		llmProvider = ollama.NewClient(ollamaURL, *modelName)
		log.Printf("Using Ollama Provider at %s (Model: %s)", ollamaURL, *modelName)
	default:
		log.Fatalf("Unknown provider: %s", *provider)
	}

	// Initialize RL Database
	rlDB, err := rl.InitDB("zenith_rl.db")
	if err != nil {
		log.Fatalf("failed to init RL database: %v", err)
	}
	defer rlDB.Close()

	// Start Background Collection
	go startScheduler(database, *collectInterval)

	// Start HTTP Server
	server := &http.Server{Addr: fmt.Sprintf(":%d", *port)}
	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		handleQuery(w, r, database, llmProvider, rlDB)
	})
	http.HandleFunc("/recommend", func(w http.ResponseWriter, r *http.Request) {
		handleRecommend(w, r, database, llmProvider, rlDB)
	})
	http.HandleFunc("/feedback", func(w http.ResponseWriter, r *http.Request) {
		handleFeedback(w, r, rlDB)
	})

	// Handle Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting Zenith Server on port %d...", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutting down Zenith Server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Zenith Server stopped.")
}

// extractPort parses a URL (e.g., http://localhost:8428) and returns the port as an int.
// If parsing fails, it returns the provided default.
func extractPort(urlStr string, defaultPort int) int {
	parts := strings.Split(urlStr, ":")
	if len(parts) >= 3 {
		// e.g., ["http", "//localhost", "8428"]
		portStr := strings.Trim(parts[len(parts)-1], "/")
		port, err := strconv.Atoi(portStr)
		if err == nil {
			return port
		}
	}
	return defaultPort
}

func startProcess(bin string, args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	// Set stdout/stderr to files or just discard if they are too chatty
	// For debugging, we can redirect to files
	logFile, err := os.OpenFile(fmt.Sprintf("%s.log", strings.TrimSuffix(filepath.Base(bin), filepath.Ext(bin))), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	log.Printf("Starting %s...", bin)
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start %s: %v", bin, err)
	}
	return cmd
}

func stopProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		log.Printf("Stopping process %d...", cmd.Process.Pid)

		// Windows doesn't support SIGTERM for child processes in the same way.
		// We'll try to be gentle but fall back to Kill quickly on Windows.
		if runtime.GOOS == "windows" {
			cmd.Process.Kill()
		} else {
			cmd.Process.Signal(syscall.SIGTERM)
		}

		// Wait for it to exit
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-done:
			log.Println("Process exited.")
		case <-time.After(3 * time.Second):
			log.Println("Process timed out, killing...")
			cmd.Process.Kill()
		}
	}
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

func handleQuery(w http.ResponseWriter, r *http.Request, database *db.VictoriaDB, client llm.Provider, rlDB *rl.DB) {
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
				id, _ := rlDB.LogExperience("query", req.Query, "", fmt.Sprintf("Failed to generate SQL: %v", err))
				respondError(w, fmt.Sprintf("Failed to generate MetricsQL after %d attempts: %v", maxRetries, err), id)
				return
			}
			continue
		}

		log.Printf("Attempt %d: Executing Query: %s", attempt, sqlQuery)

		if strings.HasPrefix(sqlQuery, "LOG:") {
			query := strings.TrimSpace(strings.TrimPrefix(sqlQuery, "LOG:"))
			results, err = database.QueryLogs(query)
		} else {
			// Default to Metrics or explicit METRIC: prefix
			query := strings.TrimSpace(strings.TrimPrefix(sqlQuery, "METRIC:"))
			results, err = database.QueryMetrics(query)
		}

		if err != nil {
			log.Printf("Attempt %d: Query Execution Error: %v", attempt, err)

			// Autonomous Self-Correction Logging: Log the failed query
			rlDB.LogExperience("query", req.Query, sqlQuery, fmt.Sprintf("Execution Error: %v", err))

			if attempt == maxRetries {
				id, _ := rlDB.LogExperience("query", req.Query, sqlQuery, fmt.Sprintf("Final Execution Error: %v", err))
				respondError(w, fmt.Sprintf("Failed to execute query after %d attempts: %v", maxRetries, err), id)
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
		id, _ := rlDB.LogExperience("query", req.Query, sqlQuery, fmt.Sprintf("Failed to explain results: %v", err))
		respondError(w, fmt.Sprintf("Failed to explain results: %v", err), id)
		return
	}

	// Log successful experience
	id, _ := rlDB.LogExperience("query", req.Query, sqlQuery, "Success")
	log.Println("Query analysis finished.")
	respondJSON(w, QueryResponse{InteractionID: id, Answer: explanation})
}

func respondJSON(w http.ResponseWriter, resp interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	log.Println("Response sent to client.")
}

func respondError(w http.ResponseWriter, msg string, id int64) {
	log.Println("Error:", msg)
	respondJSON(w, QueryResponse{InteractionID: id, Error: msg})
}

func handleRecommend(w http.ResponseWriter, r *http.Request, database *db.VictoriaDB, client llm.Provider, rlDB *rl.DB) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("Generating recommendations...")

	var systemDataBuilder strings.Builder

	// CPU
	cpuRes, err := database.QueryMetrics("avg(cpu_usage_pct)")
	if err == nil {
		systemDataBuilder.WriteString(fmt.Sprintf("Global Avg CPU: %s\n", cpuRes))
	}

	// Memory
	memRes, err := database.QueryMetrics("avg(memory_used_mb)")
	if err == nil {
		systemDataBuilder.WriteString(fmt.Sprintf("Global Avg Memory Used (MB): %s\n", memRes))
	}

	// Top Processes by CPU
	topCPU, err := database.QueryMetrics("topk(5, process_cpu_pct)")
	if err == nil {
		systemDataBuilder.WriteString(fmt.Sprintf("Top 5 Processes by CPU:\n%s\n", topCPU))
	}

	// Top Processes by Memory
	topMem, err := database.QueryMetrics("topk(5, process_memory_mb)")
	if err == nil {
		systemDataBuilder.WriteString(fmt.Sprintf("Top 5 Processes by Memory:\n%s\n", topMem))
	}

	// Recent Error Logs
	errLogs, err := database.QueryLogs(`* | filter eventMessage: "error" OR messageType: "error" | limit 10`)
	if err == nil {
		systemDataBuilder.WriteString(fmt.Sprintf("Recent Error Logs:\n%s\n", errLogs))
	}

	systemData := systemDataBuilder.String()
	log.Printf("System Data for Recommendations:\n%s", systemData)

	recommendations, err := client.GenerateRecommendations(systemData)
	if err != nil {
		id, _ := rlDB.LogExperience("recommend", "Generate system recommendations", "", fmt.Sprintf("Failed to generate recommendations: %v", err))
		respondError(w, fmt.Sprintf("Failed to generate recommendations: %v", err), id)
		return
	}

	id, _ := rlDB.LogExperience("recommend", "Generate system recommendations", "", "Success")
	log.Println("Recommendations generated successfully.")
	respondJSON(w, QueryResponse{InteractionID: id, Answer: recommendations})
}

// FeedbackRequest defines the payload for submitting RL feedback.
type FeedbackRequest struct {
	InteractionID int64 `json:"interaction_id"`
	Feedback      int   `json:"feedback"` // 1 = good, -1 = bad
}

func handleFeedback(w http.ResponseWriter, r *http.Request, rlDB *rl.DB) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := rlDB.UpdateFeedback(req.InteractionID, req.Feedback); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update feedback: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}
