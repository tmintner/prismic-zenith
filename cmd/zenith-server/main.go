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
	"strings"
	"syscall"
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
	metricsURL := flag.String("metrics-url", "http://localhost:8428", "VictoriaMetrics URL")
	logsURL := flag.String("logs-url", "http://localhost:9428", "VictoriaLogs URL")
	// Default paths based on OS
	defaultMetricsBin := "/opt/homebrew/bin/victoria-metrics"
	defaultLogsBin := "/opt/homebrew/bin/victoria-logs"
	if runtime.GOOS == "windows" {
		defaultMetricsBin = "victoria-metrics.exe"
		defaultLogsBin = "victoria-logs.exe"
	}

	metricsBin := flag.String("metrics-bin", defaultMetricsBin, "Path to victoria-metrics binary")
	logsBin := flag.String("logs-bin", defaultLogsBin, "Path to victoria-logs binary")
	metricsData := flag.String("metrics-data", "./vm-data", "Path to VictoriaMetrics data")
	logsData := flag.String("logs-data", "./vlogs-data", "Path to VictoriaLogs data")

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

	// Start VictoriaMetrics and VictoriaLogs
	metricsCmd := startProcess(*metricsBin, "-storageDataPath", *metricsData, "-httpListenAddr", ":8428")
	defer stopProcess(metricsCmd)

	logsCmd := startProcess(*logsBin, "-storageDataPath", *logsData, "-httpListenAddr", ":9428")
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
	server := &http.Server{Addr: fmt.Sprintf(":%d", *port)}
	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		handleQuery(w, r, database, llmProvider)
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
