# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build both binaries for macOS (output to bin/)
make build-mac

# Build for Windows (cross-compile)
make build-windows

# Build individual targets
make build-server-mac
make build-cli-mac

# Clean binaries
make clean
```

macOS builds require `CGO_ENABLED=1` (the default on native builds). The `GEMINI_API_KEY` env var or Makefile variable is baked into the binary via `-ldflags`.

## Running Tests

```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./pkg/db/...

# Run a single test
go test ./pkg/db/ -run TestVictoriaDB_QueryLogs
```

Tests use `httptest.NewServer` to mock the VictoriaMetrics/VictoriaLogs HTTP APIs — no live backends needed.

## Running the System

1. Copy `config.json.example` to `config.json` and fill in paths/keys.
2. Start the server (it auto-launches VictoriaMetrics and VictoriaLogs as subprocesses):
   ```bash
   ./bin/zenith-server
   ```
3. Query via CLI:
   ```bash
   ./bin/zenith-cli "What was the average CPU usage in the last hour?"
   ./bin/zenith-cli recommend
   ./bin/zenith-cli --id 42 --feedback good
   ```

## Architecture

### Two Binaries

- **`cmd/zenith-server`** — Background daemon. Starts VictoriaMetrics and VictoriaLogs as child processes, runs a scheduler that collects metrics/logs every 5 minutes (SRUM hourly on Windows), and exposes an HTTP API on port 8080.
- **`cmd/zenith-cli`** — Thin CLI client. Sends natural language queries to the server and prints results. Supports `recommend` and `--feedback` subcommands.

### HTTP API (zenith-server)

| Endpoint | Method | Description |
|---|---|---|
| `/query` | POST | Natural language → LLM → MetricsQL/LogsQL → results |
| `/recommend` | GET/POST | Proactive system health recommendations |
| `/feedback` | POST | Submit `good`/`bad` feedback on an interaction ID |

### LLM Query Flow

The LLM (`pkg/gemini` or `pkg/ollama`) translates a natural language query into a single line prefixed with either `METRIC:` or `LOG:`. The server strips the prefix and routes to `VictoriaDB.QueryMetrics()` or `VictoriaDB.QueryLogs()` accordingly. Failed queries are retried up to 3 times. All interactions are logged to `zenith_rl.db` (SQLite) for feedback tracking.

### Key Packages

- **`pkg/config`** — Loads `config.json` with OS-aware defaults. Missing file is non-fatal; all fields have defaults.
- **`pkg/db`** — `VictoriaDB` wraps VictoriaMetrics (`/api/v1/import/prometheus`, `/api/v1/query`) and VictoriaLogs (`/insert/jsonline`, `/select/logsql/query`). Metrics use Prometheus text format; logs use NDJSON. The query step is hardcoded to `4200` (70 min) to bridge the gap between 5-minute regular collection and 1-hour SRUM collection cycles.
- **`pkg/collector`** — Platform-specific via Go build tags (`//go:build darwin` / `//go:build windows`). Implements `CollectLogs`, `CollectMetrics`, `CollectProcessMetrics`, and `CollectSrumHistoricalMetrics`. On macOS, logs come from `log show --style json`. On Windows, SRUM data is read from `C:\Windows\System32\sru\SRUDB.dat` by creating a VSS shadow copy (to bypass the DiagTrack lock), then parsing the ESE database format.
- **`pkg/llm`** — `Provider` interface with three methods: `GenerateSQL`, `ExplainResults`, `GenerateRecommendations`.
- **`pkg/gemini`** / **`pkg/ollama`** — Implement `llm.Provider`. Gemini uses `gemini-3-flash-preview`. The prompt engineering in `gemini/client.go` is critical — LLM rules define valid metric names, label conventions, and query syntax.
- **`pkg/rl`** — SQLite (`zenith_rl.db`) experience replay store. Every query/recommendation logs prompt, generated query, and result. Users can later submit feedback tied to an interaction ID.

### Platform-Specific Details

- **macOS**: `CGO_ENABLED=1` required. Process metrics filter to RSS > 50 MB to reduce noise.
- **Windows**: SRUM collection uses `internal/go-ese-patched` (a local fork of `www.velocidex.com/golang/go-ese` via `go.mod` replace directive) to parse the ESE database format. The `SruDbIdMapTable` maps integer IDs to app paths and user SIDs. SRUM is collected hourly; live per-process disk I/O (`collectProcessIOMetrics`) is collected every 5 minutes via `GetProcessIoCounters`.

### Configuration

All settings live in `config.json` (see `config.json.example`). Key fields:

- `llm_provider`: `"gemini"` or `"ollama"`
- `metrics_bin` / `logs_bin`: Paths to VictoriaMetrics and VictoriaLogs binaries
- `collect_interval`: Duration string (e.g. `"5m"`)
- `gemini_api_key`: Can also be set via `GEMINI_API_KEY` env var (takes precedence)
