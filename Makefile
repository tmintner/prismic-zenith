# Build Configuration
BINARY_NAME=zenith
CLI_BINARY_NAME=zenith-cli
SERVER_BINARY_NAME=zenith-server
OUT_DIR=bin
API_KEY?=$(GEMINI_API_KEY)
LDFLAGS=-ldflags "-X main.DefaultAPIKey=$(API_KEY)"

.PHONY: all clean build-mac build-windows build-cli-mac build-cli-windows build-server-mac build-server-windows

all: build-mac build-windows

# Build everything for macOS
build-mac: build-server-mac build-cli-mac

# Build everything for Windows
build-windows: build-server-windows build-cli-windows

# Server Builds
build-server-mac:
	@echo "Building Zenith Server for macOS..."
	@mkdir -p $(OUT_DIR)
	go build $(LDFLAGS) -o $(OUT_DIR)/$(SERVER_BINARY_NAME) ./cmd/zenith-server

build-server-windows:
	@echo "Building Zenith Server for Windows..."
	@mkdir -p $(OUT_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/$(SERVER_BINARY_NAME).exe ./cmd/zenith-server

# CLI Builds
build-cli-mac:
	@echo "Building Zenith CLI for macOS..."
	@mkdir -p $(OUT_DIR)
	go build -o $(OUT_DIR)/$(CLI_BINARY_NAME) ./cmd/zenith-cli

build-cli-windows:
	@echo "Building Zenith CLI for Windows..."
	@mkdir -p $(OUT_DIR)
	GOOS=windows GOARCH=amd64 go build -o $(OUT_DIR)/$(CLI_BINARY_NAME).exe ./cmd/zenith-cli

# Clean binaries
clean:
	@echo "Cleaning up..."
	@rm -rf $(OUT_DIR)
	@rm -f zenith-server zenith-cli zenith-server.exe zenith-cli.exe
