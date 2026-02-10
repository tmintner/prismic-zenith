# Zenith: AI-Powered System Monitor

Zenith is a cross-platform AI agent that monitors your system (macOS/Windows) and lets you ask questions about its behavior using natural language. It runs as a background service and provides a CLI for interaction.

## Features

- **Background Service**: Automatically collects system logs and metrics every 5 minutes (configurable).
- **Cross-Platform**:
    - **macOS**: Unified Logging (`log show`) and `top`.
    - **Windows**: Event Logs (`Get-WinEvent`) and Performance Counters (`typeperf`).
- **AI Analysis**: Uses Google Gemini to translate your questions into SQL queries against the local database.
- **Pure-Go SQLite**: No CGO required, making cross-compilation easy.

## Components

1.  **Zenith Server (`zenith-server`)**: The core daemon that collects data and exposes an HTTP API.
2.  **Zenith CLI (`zenith-cli`)**: A lightweight client to query the server.

## Installation

### Build from Source

You can build the binaries for your current platform:

```bash
# Build Server
go build -o zenith-server ./cmd/zenith-server

# Build CLI
go build -o zenith-cli ./cmd/zenith-cli
```

### Cross-Compile for Windows (from Mac/Linux)

```bash
GOOS=windows GOARCH=amd64 go build -o zenith-server.exe ./cmd/zenith-server
GOOS=windows GOARCH=amd64 go build -o zenith-cli.exe ./cmd/zenith-cli
```
### Build with Makefile (Recommended)

The easiest way to build for all platforms is using the included `Makefile`.

```bash
# Build everything (macOS and Windows)
make all API_KEY=YOUR_SECRET_KEY

# Build just for macOS
make build-mac API_KEY=YOUR_SECRET_KEY

# Build just for Windows
make build-windows API_KEY=YOUR_SECRET_KEY

# Clean up build artifacts
make clean
```

> [!TIP]
> If you have `GEMINI_API_KEY` set in your environment, you can just run `make` and it will automatically pick it up.

### Manual Build with Embedded API Key

> [!IMPORTANT]
> While this hides the key from your source code/Git repository, it is **NOT** perfectly safe if you distribute the binary publicly. Anyone with basic tools can extract strings from a compiled binary. If this is a private tool, this method is excellent and secure.
## Usage

### 1. Start the Server

Run the server to start data collection. It needs your Gemini API key.

```bash
export GEMINI_API_KEY="your-api-key"
./zenith-server
```

**Options:**
-   `-port`: HTTP server port (default 8080).
-   `-interval`: Collection interval (e.g., `10m`, `1h`) (default `5m`).
-   `-key`: Gemini API Key (if not set via environment variable).

### 2. Query with the CLI

Ask questions about your system status.

```bash
./zenith-cli "What errors occurred in the last hour?"
```

**Options:**
-   `-server`: Address of the Zenith Server (default `http://localhost:8080`).

## Database Schema

Data is stored in `zenith.db` in the working directory of the server.

-   **`system_logs`**: Capture of system events (timestamp, process, level, message).
-   **`system_metrics`**: Snapshots of CPU usage and memory stats.
