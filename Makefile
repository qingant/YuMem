.PHONY: build clean install test run

# Build configuration
APP_NAME = yumem
BUILD_DIR = build
MAIN_PACKAGE = ./cmd

# Go build flags for single executable
LDFLAGS = -ldflags="-s -w"
BUILD_FLAGS = -a -installsuffix cgo

# Default target
build: clean
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PACKAGE)
	@echo "✓ Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Build for multiple platforms
build-all: clean
	@echo "Building $(APP_NAME) for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	
	# macOS
	GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 $(MAIN_PACKAGE)
	GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	
	# Linux
	GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 $(MAIN_PACKAGE)
	GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 $(MAIN_PACKAGE)
	
	# Windows
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe $(MAIN_PACKAGE)
	
	@echo "✓ Multi-platform build complete"

# Clean build directory
clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILD_DIR)

# Install to local bin
install: build
	@echo "Installing $(APP_NAME) to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(APP_NAME) /usr/local/bin/
	@echo "✓ $(APP_NAME) installed successfully"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run the application
run: build
	./$(BUILD_DIR)/$(APP_NAME)

# Initialize go modules
mod-init:
	go mod init yumem

# Download dependencies
mod-tidy:
	go mod tidy

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build single executable"
	@echo "  build-all  - Build for multiple platforms" 
	@echo "  clean      - Clean build directory"
	@echo "  install    - Install to /usr/local/bin"
	@echo "  test       - Run tests"
	@echo "  run        - Build and run"
	@echo "  mod-tidy   - Download dependencies"
	@echo "  help       - Show this help"