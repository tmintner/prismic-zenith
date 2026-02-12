package config

import (
	"encoding/json"
	"os"
	"runtime"
)

type Config struct {
	ServerPort      int    `json:"server_port"`
	MetricsPort     int    `json:"metrics_port"`
	LogsPort        int    `json:"logs_port"`
	OllamaPort      int    `json:"ollama_port"`
	MetricsBin      string `json:"metrics_bin"`
	LogsBin         string `json:"logs_bin"`
	MetricsData     string `json:"metrics_data"`
	LogsData        string `json:"logs_data"`
	LLMProvider     string `json:"llm_provider"`
	OllamaModel     string `json:"ollama_model"`
	CollectInterval string `json:"collect_interval"`
	GeminiAPIKey    string `json:"gemini_api_key"`
}

func LoadConfig(path string) (*Config, error) {
	// Defaults based on OS
	metricsBin := "/opt/homebrew/bin/victoria-metrics"
	logsBin := "/opt/homebrew/bin/victoria-logs"
	if runtime.GOOS == "windows" {
		metricsBin = "victoria-metrics.exe"
		logsBin = "victoria-logs.exe"
	}

	cfg := &Config{
		ServerPort:      8080,
		MetricsPort:     8428,
		LogsPort:        9428,
		OllamaPort:      11434,
		MetricsBin:      metricsBin,
		LogsBin:         logsBin,
		MetricsData:     "./vm-data",
		LogsData:        "./vlogs-data",
		LLMProvider:     "gemini",
		OllamaModel:     "phi4-mini",
		CollectInterval: "5m",
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
