# Zenith: AI-Powered System Monitor

Zenith is a cross-platform (macOS/Windows) AI agent that monitors your system and allows you to ask questions about its behavior using natural language. It collects metrics and logs into a high-performance time-series backend and uses LLMs to interpret your queries.

## Features

- **Continuous Monitoring**: Automatically collects system metrics and logs at configurable intervals (default: 5 minutes).
- **Cross-Platform Support**:
    - **macOS**: Native Unified Logging via CGO and `top` replacement using `gopsutil`.
    - **Windows**: Native Event Logs (`EvtQuery`) and SRUM parsing using direct ESE database access.
- **AI-Driven Analysis**: Translates natural language questions into MetricsQL (for metrics) or LogSQL (for logs) using Google Gemini, Ollama, or a built-in `llama.cpp` integration.
- **System Recommendations**: Proactively analyzes system health (CPU, Memory, error logs) to provide actionable optimization tips.
- **High-Performance Storage**: Uses **VictoriaMetrics** for metrics and **VictoriaLogs** for log entries.
- **Zero-Setup AI**: The `llama.cpp` provider automatically downloads a default model (Phi-4-mini) on first run if you don't provide your own, allowing for immediate offline analysis without complex setup.
- **Desktop GUI**: Native webview window with live CPU/memory gauges, top-process tables, and an AI chat interface.
- **Configurable**: Fully manageable via `config.json` or environment variables.

## Components

1.  **Zenith Server (`zenith-server`)**: The background daemon responsible for data collection and exposing the query API.
2.  **Zenith CLI (`zenith-cli`)**: A command-line tool to query the system state.
3.  **Zenith GUI (`zenith-gui`)**: A desktop application with a live dashboard and AI chat interface.
4.  **VictoriaMetrics & VictoriaLogs**: The backend databases for storing system data.

---

## Installation & Setup

### 1. Prerequisites

- **Go 1.24+**: Required for building from source.
- **CGO**: Required for macOS builds to interface with the System Log API and the GUI's WebKit backend.

> [!IMPORTANT]
> macOS builds require `CGO_ENABLED=1` (default on native builds) to support unified logging and the webview GUI.
- **VictoriaMetrics**: [Download](https://victoriametrics.com/limited-binaries/) or install via Homebrew: `brew install victoria-metrics`.
- **VictoriaLogs**: [Download](https://docs.victoriametrics.com/victorialogs/) or install via Homebrew: `brew install victoria-logs`.
- **Windows (GUI only)**: Edge WebView2 runtime — pre-installed on Windows 10 1803+ and Windows 11.

### 2. Configuration

Create a `config.json` in the root directory. You can use `config.json.example` as a template:

```json
{
    "server_host": "localhost",
    "server_port": 8080,
    "metrics_host": "localhost",
    "metrics_port": 8428,
    "logs_host": "localhost",
    "logs_port": 9428,
    "ollama_host": "localhost",
    "ollama_port": 11434,
    "metrics_bin": "/opt/homebrew/bin/victoria-metrics",
    "logs_bin": "/opt/homebrew/bin/victoria-logs",
    "metrics_data": "./vm-data",
    "logs_data": "./vlogs-data",
    "llm_provider": "llamacpp",
    "ollama_model": "phi4-mini",
    "llamacpp_host": "localhost",
    "llamacpp_port": 8080,
    "llamacpp_bin": "llama-server",
    "llamacpp_model": "./models/Phi-4-mini-instruct-Q4_K_M.gguf",
    "collect_interval": "5m",
    "gemini_api_key": "YOUR_GEMINI_API_KEY_HERE"
}
```

> [!TIP]
> If you set `"llm_provider": "llamacpp"` and leave the `llamacpp_model` field empty or pointing to a non-existent file, Zenith will automatically download the Phi-4-mini 3.8B model on its first startup.

> [!TIP]
> You can also set `GEMINI_API_KEY` as an environment variable to avoid storing it in plain text.

### 3. Build from Source

Using the provided Makefile is the easiest way to build for your platform:

```bash
# Build all three binaries for macOS (server, CLI, GUI)
make build-mac

# Build all three binaries for Windows
make build-windows

# Build individual targets
make build-server-mac
make build-cli-mac
make build-gui-mac
```

Binaries are written to `bin/`.

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

### 3. Open the GUI

```bash
./bin/zenith-gui
```

The window shows:
- **Live dashboard** — CPU and memory gauges updated every 10 seconds, plus top-5 processes by CPU and memory.
- **AI chat** — type any natural language question and press Send. Thumbs-up/down buttons on each response send feedback back to the server.
- **Recommendations** — click the Recommend button for a proactive health analysis.

The GUI requires `zenith-server` to be running first. If the server is unreachable, it displays a connection error in the chat area rather than crashing.

### 4. Query via CLI

Use the CLI to ask questions about your system.

```bash
# Using default server address (from config.json)
./bin/zenith-cli "What was the average CPU usage in the last hour?"

# Specifying server address as the first positional argument
./bin/zenith-cli localhost:8080 "What was the average CPU usage in the last hour?"

# Using a full URL as a positional argument
./bin/zenith-cli http://192.168.1.5:8080 recommend
```

### 5. System Recommendations

Zenith can proactively analyze your system's metrics and logs to provide recommendations.

```bash
./bin/zenith-cli recommend
```

**Example Output:**
> --- Zenith Recommendations ---
> Based on the current system state:
> 1. **High CPU Usage**: The process 'Browser' is consuming 25% CPU. Consider closing unused tabs.
> 2. **Memory Pressure**: Global memory usage is at 85%. You may experience slowdowns.
> 3. **Error Alerts**: Found 3 Disk I/O errors in the last hour. A hardware check is recommended.

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
- `process_cpu_pct`: Per-process CPU usage (labels: `pid`, `process_name`).
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

### Windows Testing (UTM)
If you have a UTM VM named `Windows11`, you can use the integrated UTM skill to automate testing.
Check `.agents/skills/utm-testing/SKILL.md` for more details on how to start the VM and run the test suite remotely.
