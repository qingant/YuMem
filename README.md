# YuMem 🧠

> **Memory is the foundation of identity. For AI to truly understand us, it must remember us as we remember ourselves.**

YuMem is a privacy-first, local memory management system that gives AI models persistent, structured memory about users. It enables AI assistants to understand you as deeply as you understand yourself, building context and relationships that grow over time.

[![Version](https://img.shields.io/badge/version-1.2.3-blue.svg)](https://github.com/qingant/YuMem)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)](https://github.com/qingant/YuMem/actions)

## What is YuMem?

YuMem bridges the memory gap between humans and AI by implementing a three-layer memory architecture:

- **L0 (Core Identity)**: Essential user information always included in conversations (< 10KB)
- **L1 (Semantic Index)**: Hierarchically organized knowledge with intelligent cross-references
- **L2 (Raw Archives)**: Complete, searchable record of all interactions and documents

Think of it as giving your AI assistant a brain that remembers, learns, and grows with each conversation.

## Key Features

### Privacy-First Architecture
- **100% Local**: All data stays on your device
- **No Telemetry**: Zero external data transmission
- **User Ownership**: Complete control over your memory data
- **Open Source**: Fully auditable and transparent

### Intelligent Memory Management
- **AI-Powered Analysis**: Automatic content categorization with OpenAI, Claude, Gemini, or GitHub Copilot
- **Natural Growth**: Memory evolves through conversations and imports
- **Smart Prioritization**: Recent and frequent information surfaces automatically
- **Semantic Organization**: Hierarchical knowledge trees with meaningful paths
- **Context-Aware Retrieval**: Intelligent assembly of relevant information across all layers
- **Auto-Consolidation**: L0 automatically consolidates when it exceeds size limits

### Seamless Integration
- **MCP Protocol**: Standard AI model integration via Streamable HTTP and stdio transports
- **CLI Interface**: Powerful command-line tools for all operations
- **Web Dashboard**: Management interface with real-time log viewer
- **Data Import**: Apple Notes (paginated), filesystem, and more

### Production Ready
- **Single Binary**: No dependencies, easy deployment
- **Cross-Platform**: Works on macOS, Linux, and Windows
- **Scalable**: Handles thousands of conversations and documents
- **Versioned**: Complete history tracking and rollback capabilities

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/qingant/YuMem.git
cd YuMem

# Build YuMem
make build

# Initialize your workspace
./build/yumem init
```

### First Run

```bash
# Start YuMem (starts MCP server + web dashboard)
./build/yumem

# Or customize ports
./build/yumem --web-port 3001 --mcp-port 8081
```

The web dashboard opens at `http://localhost:1607` and the MCP server listens at `http://localhost:1229/mcp`.

### Set Your Profile

```bash
# Add your basic information
./build/yumem l0 set --name "Your Name" --context "Your role/context"

# View your L0 profile
./build/yumem l0 show
```

### Configure AI Provider (Optional but Recommended)

```bash
# Configure Gemini (default provider)
./build/yumem ai setup --provider gemini --api-key YOUR_GEMINI_API_KEY

# Or configure OpenAI
./build/yumem ai setup --provider openai --api-key YOUR_OPENAI_API_KEY

# Or configure Claude
./build/yumem ai setup --provider claude --api-key YOUR_CLAUDE_API_KEY

# View configured providers
./build/yumem ai list
```

**Note**: Without an AI provider, YuMem uses local heuristics for content analysis. For optimal performance and intelligent categorization, configure an AI provider.

## Usage Guide

### Command Line Interface

```bash
# Initialize workspace
yumem init

# Start server (MCP + Web Dashboard)
yumem
yumem --mcp-stdio  # stdio mode for Claude Desktop / Cursor

# Configure AI provider for intelligent analysis
yumem ai setup --provider gemini --api-key YOUR_KEY
yumem ai list

# Manage L0 (core identity)
yumem l0 set --name "John Doe" --context "Software Engineer"
yumem l0 show
yumem l0 consolidate

# Manage L1 (semantic index)
yumem l1 create --path "work/projects/yumem" --title "Memory System Project"
yumem l1 search "machine learning"
yumem l1 tree

# Manage L2 (raw content)
yumem l2 add document.txt --tags "important,work"
yumem l2 search "neural networks"
yumem l2 list

# Import data sources (AI-powered analysis)
yumem import notes --limit 100
yumem import files --path ~/Documents --recursive

# High-level memory operations
yumem memory core     # What chatbots see at conversation start
yumem memory recall "topic"  # Semantic search with AI

# Reset workspace
yumem reset
```

### Web Dashboard

Access the web dashboard at `http://localhost:1607` (default) for:
- System overview and statistics
- Memory layer browsing (L0/L1/L2)
- Memory tools testing (core memory, recall, store)
- Prompt template management
- AI provider configuration
- Real-time log viewer with filtering
- Data export and system settings

### MCP Integration

YuMem exposes 13 MCP tools for AI integration:

| Tool | Description |
|------|-------------|
| `get_l0_context` | Retrieve core user profile |
| `update_l0` | Update identity/preferences |
| `consolidate_l0` | Deduplicate and narrativize traits |
| `search_l1` | Search semantic knowledge index |
| `create_l1_node` | Create new knowledge node |
| `update_l1_node` | Update node summary/keywords |
| `search_l2` | Search raw content archive |
| `add_l2_file` | Add file to archive |
| `get_l2_content` | Retrieve file content by ID |
| `retrieve_context` | Intelligent context assembly |
| `store_memory` | Store conversations or notes |
| `recall_memory` | Semantic search with AI ranking |
| `get_core_memory` | Get user profile for conversation start |

## Architecture Overview

### Three-Layer Memory Model

```
┌─────────────────────────────────────────┐
│             L0: Core Identity           │
│    - Personal traits and preferences    │
│    - Current agenda and focus areas     │
│    - Essential context (< 10KB)         │
├─────────────────────────────────────────┤
│           L1: Semantic Index            │
│    - Hierarchical knowledge paths       │
│    - Cross-referenced summaries         │
│    - Intelligent categorization         │
├─────────────────────────────────────────┤
│            L2: Raw Archives             │
│    - Complete conversation history      │
│    - Full document storage              │
│    - Searchable content database        │
└─────────────────────────────────────────┘
```

### Service Architecture

```
┌──────────────────────────────────────────┐
│              YuMem Binary                │
├──────────────┬───────────────────────────┤
│  MCP Server  │    Web Dashboard          │
│  (port 1229) │    (port 1607)            │
│  Streamable  │    Go templates +         │
│  HTTP + stdio│    Tailwind CSS           │
├──────────────┴───────────────────────────┤
│         Context Retrieval Engine         │
├──────────┬──────────┬────────────────────┤
│ L0 Mgr   │ L1 Mgr   │ L2 Mgr            │
├──────────┴──────────┴────────────────────┤
│  AI Providers │ Prompts │ Versioning     │
├──────────────────────────────────────────┤
│           File System Storage            │
│        (workspace/_yumem/...)            │
└──────────────────────────────────────────┘
```

### Workspace Directory Structure

```
workspace/
└── _yumem/
    ├── l0/current/        # Core user identity
    │   ├── traits.json    # Personality, skills, philosophy
    │   ├── agenda.json    # Current focus and priorities
    │   └── meta.json      # System metadata
    ├── l1/
    │   ├── index.json     # Node index
    │   └── nodes/         # Individual semantic nodes
    ├── l2/
    │   ├── index.json     # Content index
    │   └── content/       # Raw files and conversations
    ├── versions/          # Version history
    └── logs/              # System logs
```

## Configuration

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

### Default Ports
- **MCP Server**: 1229 (override with `--mcp-port`)
- **Web Dashboard**: 1607 (override with `--web-port`)

### Environment Variables
- `YUMEM_WORKSPACE`: Override workspace directory
- Command line flags override config file settings

## Integration Examples

### Claude Desktop (stdio mode - recommended)

Add to your Claude Desktop MCP configuration (`~/Library/Application Support/Claude/claude_desktop_config.json`):

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

### Cursor (stdio mode)

Add to Cursor's MCP settings:

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

### Python Integration (HTTP mode)

Start YuMem in server mode first (`./build/yumem`), then connect via HTTP:

```python
import requests

class YuMemClient:
    def __init__(self, base_url="http://localhost:1229"):
        self.base_url = base_url

    def mcp_call(self, method, params=None):
        """Send an MCP request via Streamable HTTP."""
        response = requests.post(f"{self.base_url}/mcp", json={
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": method,
                "arguments": params or {}
            }
        })
        return response.json()

    def get_context(self):
        return self.mcp_call("get_l0_context")

    def search_memory(self, query):
        return self.mcp_call("search_l1", {"query": query})

    def store_note(self, content, source="api"):
        return self.mcp_call("store_memory", {
            "content": content,
            "source": source
        })

# Usage
memory = YuMemClient()
context = memory.get_context()
results = memory.search_memory("machine learning projects")
```

## Performance & Scaling

### System Requirements
- **RAM**: 50-100MB typical usage
- **Storage**: Grows with usage (typically 1-10GB)
- **CPU**: Minimal background usage
- **Network**: Local-only, no external dependencies

### Performance Characteristics
- **L0 Loading**: < 10ms (always < 10KB)
- **L1 Search**: < 100ms for 10,000 nodes
- **L2 Query**: < 500ms for 100,000 entries
- **Context Assembly**: < 200ms typical

## Security & Privacy

### Data Protection
- **Local Storage Only**: Data never leaves your device
- **File Permissions**: Restricted access to workspace directory
- **No Telemetry**: Zero external communication
- **Audit Logs**: Complete operation tracking via logging system
- **Sensitive Data Scrubbing**: L0 consolidation automatically removes API keys, passwords, and personal IDs

### Privacy Features
- **User Ownership**: You control all data
- **Transparent Operations**: All actions are logged and explainable
- **Data Portability**: JSON/text formats for easy migration
- **Selective Sharing**: Choose what to share, if anything

## Contributing

We welcome contributions that enhance user privacy, improve performance, and expand AI model compatibility!

### Development Setup

```bash
# Clone and setup
git clone https://github.com/qingant/YuMem.git
cd YuMem

# Install dependencies
go mod download

# Run tests
make test

# Build and test
make build
./build/yumem init
./build/yumem --help
```

### Areas We Need Help
- **Internationalization**: Multi-language support
- **Integrations**: More AI model and platform integrations
- **Analytics**: Advanced memory usage analytics
- **UI/UX**: Dashboard improvements and themes
- **Importers**: Support for more data sources (Notion, Obsidian, Evernote)
- **Testing**: Expanded test coverage for retrieval, MCP, and web APIs

## Documentation

### Deep Dive Documentation
- [**Design Document**](docs/DESIGN.md): Complete architecture and implementation details
- [**Principles & Philosophy**](docs/PRINCIPLES.md): Project vision and design principles

## Why YuMem?

### The Problem
Today's AI conversations are ephemeral. Each interaction starts from scratch, lacking the rich context that makes human relationships meaningful. AI assistants can't remember your preferences, learn from past conversations, or build on previous insights.

### The Solution
YuMem gives AI systems structured, persistent memory that grows with each interaction. It's like giving your AI assistant a brain that remembers, learns, and evolves—while keeping all your data private and under your control.

### The Vision
We're building the foundation for AI relationships that feel as natural and contextual as human ones. Your AI assistant should know your working style, remember your goals, understand your communication preferences, and grow alongside you over time.

## The Story Behind YuMem

YuMem gets its name from "小愚" (Xiao Yu) - the creator's son's name. Just as a parent remembers every detail of their child's growth, development, and personality, YuMem enables AI to develop that same deep, persistent understanding of you.

The name embodies the project's core philosophy: memory is not just data storage, but the foundation of meaningful relationships and understanding.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

**Ready to give your AI a memory?**

```bash
git clone https://github.com/qingant/YuMem.git
cd YuMem
make build
./build/yumem init
```

---

*Join us in building the future of human-AI memory. Your conversations, your data, your control—but with the power of perfect recall and infinite patience.*
