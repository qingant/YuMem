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

# Run in stdio mode (for Claude Desktop / Cursor MCP integration)
./build/yumem --mcp-stdio
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
- **L0 Layer**: Core user identity (traits, agenda, metadata) - size-limited to 10KB, auto-consolidates on overflow
- **L1 Layer**: Semantic index with hierarchical paths for organized knowledge
- **L2 Layer**: Raw content archive with full-text search capabilities

### Key Components
- **CLI Interface**: `internal/cli/` - Cobra-based command system
- **Memory Management**: `internal/memory/` - L0, L1, L2 managers
- **MCP Server**: `internal/mcp/` - AI model integration via MCP protocol (Streamable HTTP + stdio transports)
- **Web Dashboard**: `internal/web/` - Management UI with Go HTML templates + Tailwind CSS
- **Import System**: `internal/importers/` - Apple Notes (paginated AppleScript), filesystem
- **AI Integration**: `internal/ai/` - Multiple provider support (OpenAI, Claude, Gemini, GitHub Copilot, local fallback)
- **Context Retrieval**: `internal/retrieval/` - Intelligent context assembly for AI conversations
- **Logging**: `internal/logging/` - Ring buffer logger with web viewer
- **Prompts**: `internal/prompts/` - Embedded default templates (source of truth), with optional user overrides at ~/.yumem/prompts/

### File Structure
```
cmd/main.go                 # Entry point
internal/
├── cli/                   # Command-line interface (Cobra)
├── memory/               # L0, L1, L2 memory managers
├── mcp/                  # MCP server (Streamable HTTP + stdio)
├── web/                  # Web dashboard server + templates
├── ai/                   # AI provider implementations
├── importers/            # Data import system (Apple Notes, filesystem)
├── retrieval/            # Context retrieval engine
├── logging/              # Global ring buffer logging system
├── workspace/            # Workspace initialization
├── config/               # Configuration management
├── versioning/           # Version management
└── prompts/              # Prompt template system (embedded defaults)
```

## Service Architecture

YuMem runs as a single binary with two operating modes:

### Server Mode (default)
Starts both services concurrently:
- **MCP Server** (default port 1229): Streamable HTTP transport at `/mcp` endpoint
- **Web Dashboard** (default port 1607): Management interface with real-time log viewer

### Stdio Mode (`--mcp-stdio`)
For direct integration with AI clients (Claude Desktop, Cursor, etc.):
- MCP Server runs over stdio transport (stdin/stdout)
- No Web Dashboard in this mode
- Configured via client's MCP settings (see Integration section below)

## MCP Tools (13 total)

The MCP server exposes these tools to AI clients:
- `get_l0_context` / `update_l0` / `consolidate_l0` - Core identity management
- `search_l1` / `create_l1_node` / `update_l1_node` - Semantic index operations
- `search_l2` / `add_l2_file` / `get_l2_content` - Raw content archive
- `retrieve_context` - Intelligent context assembly across all layers
- `store_memory` - Store conversations (session-based) or standalone notes
- `recall_memory` - Semantic search with AI-powered ranking
- `get_core_memory` - Get user profile for conversation start

## Development Patterns

### CLI Commands Structure
Commands are organized hierarchically:
- `yumem` - Main server (starts both MCP and web services)
- `yumem --mcp-stdio` - Stdio mode for AI client integration
- `yumem init` - Initialize workspace
- `yumem l0 set/show/consolidate` - Manage core identity
- `yumem l1 create/search/tree` - Manage semantic index
- `yumem l2 add/search/list` - Manage raw content
- `yumem ai setup/list` - Configure AI providers
- `yumem import notes/files` - Import data sources
- `yumem memory core/recall` - High-level memory operations
- `yumem reset` - Reset workspace data

### Memory Layer Interaction
- L0 data is size-controlled (10KB) with auto-consolidation on overflow
- L1 nodes reference L2 entries via IDs
- L2 stores complete content with metadata and deduplication (MD5 hash)
- Context retrieval intelligently assembles information across all layers
- Import pipeline: Content → L2 storage → AI analysis → L0 traits/agenda + L1 node creation

### AI Provider Integration
The system supports multiple AI providers through a common `Provider` interface:
- OpenAI (GPT-4o, GPT-4-turbo)
- Anthropic Claude (Claude Sonnet 4, Claude Haiku 4.5)
- Google Gemini (gemini-2.0-flash, gemini-2.5-flash-preview, gemini-2.5-pro-preview)
- GitHub Copilot (GPT-4o via GitHub API)
- Local fallback (heuristic-based, no API required)

### Data Storage
- All data under `_yumem/` directory within the workspace
- JSON for structured data (L0, L1/L2 indexes)
- Plain text for L2 content files
- File-based storage with atomic writes
- Append-only design for L1/L2 layers

### Logging System
- Ring buffer logger (`internal/logging/`) with configurable size (default 2000 entries)
- Four levels: DEBUG, INFO, WARN, ERROR
- Component tagging (cli, mcp, web, import, memory, ai)
- Web viewer at `/logs` with auto-refresh polling (2s), filters, and search
- API endpoint: `GET /api/logs?level=&component=&q=&since_id=&limit=`

## Key Configuration

### Config File (~/.yumem.yaml)
```yaml
workspace_dir: ~/yumem-workspace
ai:
  default_provider: gemini
  providers:
    gemini:
      type: gemini
      api_key: YOUR_GEMINI_API_KEY
      model: gemini-2.0-flash
    openai:
      type: openai
      api_key: YOUR_OPENAI_API_KEY
      model: gpt-4o
    claude:
      type: claude
      api_key: YOUR_CLAUDE_API_KEY
      model: claude-sonnet-4-20250514
    local:
      type: local
```

### Workspace Directory Structure
```
workspace/
└── _yumem/
    ├── l0/current/        # Core identity (traits.json, agenda.json, meta.json)
    ├── l1/
    │   ├── index.json     # Node index
    │   └── nodes/         # Individual node files
    ├── l2/
    │   ├── index.json     # Content index
    │   └── content/       # Raw content files
    ├── versions/          # Version history manifests
    └── logs/              # System log files
```

### Prompt Templates
- **Source of truth**: `internal/prompts/defaults/` — embedded into the binary at build time via `//go:embed`
- **User overrides** (optional): `~/.yumem/prompts/` — if a file exists here, it takes priority over the embedded default
- **Loading priority**: user override on disk > embedded default in binary (see `LoadTemplateFile` / `LoadPrompt` in `internal/prompts/manager.go`)
- **To edit prompts**: modify files in `internal/prompts/defaults/`, then `make build`. No need to copy files anywhere
- Categories: `import` (content analysis), `l0` (consolidation), `retrieval` (tree search)

### Environment Variables
- `YUMEM_WORKSPACE`: Override workspace directory
- Command line flags override config file settings

## Integration

### Claude Desktop / Cursor (stdio mode)
```json
{
  "mcpServers": {
    "yumem": {
      "command": "/path/to/yumem",
      "args": ["--mcp-stdio", "--workspace", "/path/to/workspace"]
    }
  }
}
```

### HTTP Mode (for custom integrations)
MCP Streamable HTTP endpoint at `http://localhost:1229/mcp` (default).

## Import System

YuMem includes comprehensive import capabilities:
- **Apple Notes**: Paginated AppleScript extraction (200 notes/page, 120s timeout per page)
- **Filesystem**: Recursive file import with extension filtering and max size limits
- **AI Analysis**: Automatic categorization, keyword extraction, L0/L1 updates when AI providers are configured
- **Auto-consolidation**: During import, if L0 exceeds 10KB, consolidation runs automatically (at most once per 10 items)

## Testing Strategy

The project follows Go testing conventions:
- Unit tests alongside source files (`*_test.go`)
- Key test files: `l0_test.go`, `l1_test.go`, `l2_optimized_test.go`, `config_test.go`
- Run tests with `make test` or `go test -v ./...`

## Known Quirks

- Viper `Unmarshal()` fails with nested AI config structs — config uses manual extraction with `viper.GetString()` for nested fields
- Gemini API responses often wrapped in markdown code blocks — `cleanAIResponse()` strips them before JSON parsing
- Apple Notes importer uses AppleScript (macOS only) with pagination to handle large note collections
- L0 traits use temporal tracking: each trait key has a timeline of `TimestampedValue` entries with validity dates and confidence scores
