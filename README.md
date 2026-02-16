# Zenith: AI-Powered System Monitor

Zenith is a cross-platform (macOS/Windows) AI agent that monitors your system and allows you to ask questions about its behavior using natural language. It collects metrics and logs into a high-performance time-series backend and uses LLMs to interpret your queries.

## Features

- **Continuous Monitoring**: Automatically collects system metrics and logs at configurable intervals (default: 5 minutes).
- **Cross-Platform Support**:
    - **macOS**: Utilizes Unified Logging (`log show`) and `top`.
    - **Windows**: Captures Event Logs (`Get-WinEvent`), Performance Counters (`typeperf`), and **SRUM (System Resource Usage Monitor)** historical data.
- **AI-Driven Analysis**: Translates natural language questions into MetricsQL (for metrics) or LogSQL (for logs) using Google Gemini or Ollama.
- **High-Performance Storage**: Uses **VictoriaMetrics** for metrics and **VictoriaLogs** for log entries.
- **Configurable**: Fully manageable via `config.json` or environment variables.

## Components

1.  **Zenith Server (`zenith-server`)**: The background daemon responsible for data collection and exposing the query API.
2.  **Zenith CLI (`zenith-cli`)**: A command-line tool to query the system state.
3.  **VictoriaMetrics & VictoriaLogs**: The backend databases for storing system data.

---

## Installation & Setup

### 1. Prerequisites

- **Go 1.24+**: Required for building from source.
- **VictoriaMetrics**: [Download](https://victoriametrics.com/limited-binaries/) or install via Homebrew: `brew install victoria-metrics`.
- **VictoriaLogs**: [Download](https://docs.victoriametrics.com/victorialogs/) or install via Homebrew: `brew install victoria-logs`.

### 2. Configuration

Create a `config.json` in the root directory. You can use `config.json.example` as a template:

```json
{
    "server_port": 8080,
    "metrics_port": 8428,
    "logs_port": 9428,
    "ollama_port": 11434,
    "metrics_bin": "/opt/homebrew/bin/victoria-metrics",
    "logs_bin": "/opt/homebrew/bin/victoria-logs",
    "metrics_data": "./vm-data",
    "logs_data": "./vlogs-data",
    "llm_provider": "gemini",
    "ollama_model": "phi4-mini",
    "collect_interval": "5m",
    "gemini_api_key": "YOUR_GEMINI_API_KEY_HERE"
}
```

> [!TIP]
> You can also set `GEMINI_API_KEY` as an environment variable to avoid storing it in plain text.

### 3. Build from Source

Using the provided Makefile is the easiest way to build for your platform:

```bash
# Build for current host platform (macOS/Windows)
make

# Build for specific platforms
make build-mac
make build-windows
```

---

## Usage

### 1. Start Backends

Ensure VictoriaMetrics and VictoriaLogs are running. If paths are correctly set in `config.json`, Zenith handles this, but you can also run them manually:

```bash
# VictoriaMetrics
victoria-metrics -storageDataPath ./vm-data -httpListenAddr :8428

# VictoriaLogs
victoria-logs -storageDataPath ./vlogs-data -httpListenAddr :9428
```

### 2. Run Zenith Server

```bash
./bin/zenith-server
```

### 3. Query via CLI

Use the CLI to ask questions about your system.

```bash
./bin/zenith-cli "What was the average CPU usage in the last hour?"
```

#### Windows & SRUM Examples
Zenith on Windows collects historical data from the System Resource Usage Monitor (SRUM).

- **Network Usage**: "Which application has used the most network bytes historically?"
- **CPU Cycles**: "Show me CPU cycle time history for Chrome."
- **Disk I/O**: "What applications have high disk read/write bytes according to SRUM?"
- **Logs**: "Were there any 'Error' level events in the System log in the last 10 minutes?"

---

## System Metrics & Logs

### Available Metrics
- `cpu_usage_pct`: Overall system CPU usage.
- `memory_used_mb` / `memory_free_mb`: System memory stats.
- `process_memory_mb`: Per-process memory usage (labels: `pid`, `process_name`).
- `srum_network_bytes_sent_total` / `srum_network_bytes_received_total`: (Windows) Network interface stats.
- `srum_app_cycle_time_total`: (Windows) Historical CPU cycles per app.
- `srum_app_bytes_read_total` / `srum_app_bytes_written_total`: (Windows) Disk I/O per app.

### Log Schema
Logs are ingested into VictoriaLogs with the following fields:
- `timestamp`: Event time.
- `processName`: Source of the log (e.g., ProviderName on Windows).
- `messageType`: Log level (e.g., LevelDisplayName on Windows, info/error on macOS).
- `eventMessage`: The actual log content.
