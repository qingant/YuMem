# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Build & Run
```bash
# Build the application
make build

# Build for multiple platforms
make build-all

# Run directly
make run

# Run with custom ports and workspace
./build/yumem --web-port 3001 --mcp-port 8081 --workspace /path/to/workspace
```

### Testing
```bash
# Run tests
make test
go test -v ./...

# Clean build artifacts
make clean
```

### Dependencies
```bash
# Download/update dependencies
go mod tidy
make mod-tidy

# Install locally
make install
```

## Project Architecture

YuMem is a privacy-first AI memory management system implemented in Go with a three-layer memory architecture:

### Core Architecture
- **L0 Layer**: Core user identity (traits, agenda, metadata) - size-limited to 10KB
- **L1 Layer**: Semantic index with hierarchical paths for organized knowledge
- **L2 Layer**: Raw content archive with full-text search capabilities

### Key Components
- **CLI Interface**: `internal/cli/` - Cobra-based command system
- **Memory Management**: `internal/memory/` - L0, L1, L2 managers
- **MCP Server**: `internal/mcp/` - AI model integration via MCP protocol
- **Web Dashboard**: `internal/web/` - Management UI with Go templates
- **Import System**: `internal/importers/` - Apple Notes, filesystem, and other data sources
- **AI Integration**: `internal/ai/` - Multiple provider support (OpenAI, Claude, Gemini, GitHub Copilot)
- **Context Retrieval**: `internal/retrieval/` - Intelligent context assembly for AI conversations

### File Structure
```
cmd/main.go                 # Entry point
internal/
├── cli/                   # Command-line interface (Cobra)
├── memory/               # L0, L1, L2 memory managers
├── mcp/                  # MCP server for AI integration
├── web/                  # Web dashboard server
├── ai/                   # AI provider implementations
├── importers/            # Data import system
├── retrieval/            # Context retrieval engine
├── workspace/            # Workspace initialization
├── config/               # Configuration management
├── versioning/           # Version management
└── prompts/              # Prompt template system
```

## Service Architecture

YuMem runs as a single binary with multiple integrated services:
- **MCP Server** (default port 8080): AI model integration endpoint
- **Web Dashboard** (default port 3000): Management interface
- **Memory Engine**: Core storage and retrieval system

## Development Patterns

### CLI Commands Structure
Commands are organized hierarchically:
- `yumem` - Main server (starts both MCP and web services)
- `yumem init` - Initialize workspace
- `yumem l0 set/show` - Manage core identity
- `yumem l1 create/search/tree` - Manage semantic index
- `yumem l2 add/search/list` - Manage raw content
- `yumem ai setup/list` - Configure AI providers
- `yumem import notes/files` - Import data sources

### Memory Layer Interaction
- L0 data is size-controlled and versioned
- L1 nodes reference L2 entries via IDs
- L2 stores complete content with metadata
- Context retrieval intelligently assembles information across all layers

### AI Provider Integration
The system supports multiple AI providers through a common interface:
- OpenAI (GPT models)
- Anthropic Claude
- Google Gemini  
- GitHub Copilot
- Local fallback (heuristic-based)

### Data Storage
- JSON for structured data (L0, L1/L2 indexes)
- Plain text for L2 content
- File-based storage with atomic writes
- Append-only design for L1/L2 layers

## Key Configuration

### Config File (~/.yumem.yaml)
```yaml
workspace: ~/yumem-workspace
ports:
  mcp: 8080
  web: 3000
ai:
  default_provider: openai
  providers:
    openai:
      type: openai
      api_key: YOUR_API_KEY
      model: gpt-4-turbo-preview
```

### Environment Variables
- `YUMEM_WORKSPACE`: Override workspace directory
- Command line flags override config file settings

## Import System

YuMem includes comprehensive import capabilities:
- **Apple Notes**: Direct SQLite database access
- **Filesystem**: Recursive file import with type filtering
- **AI Analysis**: Automatic categorization and keyword extraction when AI providers are configured

## Testing Strategy

The project follows Go testing conventions:
- Unit tests alongside source files (`*_test.go`)
- Integration tests for full workflows
- Run tests with `make test` or `go test -v ./...`

## Development Notes

- The system initializes workspace structure automatically on first run
- All data remains local by default (privacy-first design)
- Web dashboard provides visual management of memory layers
- MCP protocol enables standard AI model integration
- Services start concurrently and shut down gracefully
- Built as single binary with no external dependencies