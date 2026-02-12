# Zenith: AI-Powered System Monitor

Zenith is a cross-platform AI agent that monitors your system (macOS/Windows) and lets you ask questions about its behavior using natural language. It runs as a background service and provides a CLI for interaction.

## Features

- **Background Service**: Automatically collects system logs and metrics every 5 minutes (configurable).
- **Cross-Platform**:
    - **macOS**: Unified Logging (`log show`) and `top`.
    - **Windows**: Event Logs (`Get-WinEvent`) and Performance Counters (`typeperf`).
- **AI Analysis**: Uses Google Gemini to translate your questions into MetricsQL (PromQL) queries against VictoriaMetrics.
- **High Performance**: Uses VictoriaMetrics for scalable, efficient storage of time-series data and system metrics.

## Components

1.  **Zenith Server (`zenith-server`)**: The core daemon that collects data and exposes an HTTP API.
2.  **Zenith CLI (`zenith-cli`)**: A lightweight client to query the server.
3.  **VictoriaMetrics**: Time-series database for metric storage (must be running).

## Installation

### Prerequisites

- **VictoriaMetrics**: Install via Homebrew: `brew install victoria-metrics`.
- **Go 1.24+**: Required for building from source.

### Build from Source

You can build the binaries for your current platform:

```bash
# Build Server
go build -o zenith-server ./cmd/zenith-server

# Build CLI
go build -o zenith-cli ./cmd/zenith-cli
```

### Build with Makefile (Recommended)

```bash
# Build everything (macOS and Windows)
make all API_KEY=YOUR_SECRET_KEY

# Build just for macOS
make build-mac API_KEY=YOUR_SECRET_KEY
```

## Usage

### 1. Start VictoriaMetrics

Ensure VictoriaMetrics is running locally:

```bash
victoria-metrics -storageDataPath ./vm-data -httpListenAddr :8428
```

### 2. Start the Server

Run the server to start data collection. It needs your Gemini API key.

```bash
export GEMINI_API_KEY="your-api-key"
./zenith-server
```

**Options:**
-   `-port`: HTTP server port (default 8080).
-   `-interval`: Collection interval (e.g., `10m`, `1h`) (default `5m`).
-   `-key`: Gemini API Key (if not set via environment variable).

### 3. Query with the CLI

Ask questions about your system status.

```bash
./zenith-cli "What is the average CPU usage?"
```

## Metrics Schema

Data is stored in VictoriaMetrics. Available metrics include:

-   `cpu_usage_pct`: Overall system CPU usage.
-   `memory_used_mb`: System memory used in MB.
-   `memory_free_mb`: System memory free in MB.
-   `process_cpu_pct`: Per-process CPU usage (labels: `pid`, `process_name`).
-   `process_memory_mb`: Per-process memory usage in MB (labels: `pid`, `process_name`).
