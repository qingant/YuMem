# YuMem 🧠

> **Memory is the foundation of identity. For AI to truly understand us, it must remember us as we remember ourselves.**

YuMem is a privacy-first, local memory management system that gives AI models persistent, structured memory about users. It enables AI assistants to understand you as deeply as you understand yourself, building context and relationships that grow over time.

[![Version](https://img.shields.io/badge/version-1.2.3-blue.svg)](https://github.com/qingant/YuMem)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)](https://github.com/qingant/YuMem/actions)

## 🎯 What is YuMem?

YuMem bridges the memory gap between humans and AI by implementing a three-layer memory architecture:

- **L0 (Core Identity)**: Essential user information always included in conversations
- **L1 (Semantic Index)**: Hierarchically organized knowledge with intelligent cross-references
- **L2 (Raw Archives)**: Complete, searchable record of all interactions and documents

Think of it as giving your AI assistant a brain that remembers, learns, and grows with each conversation.

## ✨ Key Features

### 🔒 **Privacy-First Architecture**
- **100% Local**: All data stays on your device
- **No Telemetry**: Zero external data transmission
- **User Ownership**: Complete control over your memory data
- **Open Source**: Fully auditable and transparent

### 🧠 **Intelligent Memory Management**
- **Natural Growth**: Memory evolves through conversations
- **Smart Prioritization**: Recent and frequent information surfaces automatically
- **Semantic Organization**: Hierarchical knowledge trees with meaningful paths
- **Context-Aware Retrieval**: Intelligent assembly of relevant information

### 🚀 **Seamless Integration**
- **MCP Protocol**: Standard AI model integration
- **CLI Interface**: Powerful command-line tools
- **Web Dashboard**: Beautiful management interface
- **Data Import**: Apple Notes, filesystem, and more

### ⚡ **Production Ready**
- **Single Binary**: No dependencies, easy deployment
- **Cross-Platform**: Works on macOS, Linux, and Windows
- **Scalable**: Handles thousands of conversations and documents
- **Versioned**: Complete history tracking and rollback capabilities

## 🚀 Quick Start

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
# Start YuMem (opens dashboard automatically)
./build/yumem

# Or customize ports
./build/yumem --web-port 3000 --mcp-port 8080
```

### Set Your Profile

```bash
# Add your basic information
./build/yumem l0 set --name "Your Name" --context "Your role/context"

# View your L0 profile
./build/yumem l0 show
```

### Test AI Integration

```bash
# Test the MCP API
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"method":"get_l0_context","params":{}}'
```

## 📖 Usage Guide

### Command Line Interface

```bash
# Initialize workspace
yumem init

# Manage L0 (core identity)
yumem l0 set --name "John Doe" --context "Software Engineer"
yumem l0 show
yumem l0 export

# Manage L1 (semantic index)
yumem l1 create --path "work/projects/yumem" --title "Memory System Project"
yumem l1 search "machine learning"
yumem l1 tree

# Manage L2 (raw content)
yumem l2 add document.txt --tags "important,work"
yumem l2 search "neural networks"
yumem l2 list

# Import data sources
yumem import notes --all
yumem import filesystem --path ~/Documents --recursive
```

### Web Dashboard

Access the web dashboard at `http://localhost:3000` for:
- 📊 System overview and statistics
- 🧠 Memory layer visualization
- 📝 Prompt template management
- 📥 Bulk import operations
- ⚙️ System configuration

### MCP API Integration

YuMem exposes a complete MCP (Model Context Protocol) API for AI integration:

```javascript
// Get user's core context
const response = await fetch('http://localhost:8080/mcp', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    method: 'get_l0_context',
    params: {}
  })
});

// Search semantic knowledge
const search = await fetch('http://localhost:8080/mcp', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    method: 'search_l1',
    params: { query: 'machine learning projects' }
  })
});
```

## 🏗️ Architecture Overview

### Three-Layer Memory Model

```
┌─────────────────────────────────────────┐
│             L0: Core Identity           │
│    • Personal traits and preferences   │
│    • Current agenda and focus areas    │
│    • Essential context (< 10KB)        │
├─────────────────────────────────────────┤
│           L1: Semantic Index            │
│    • Hierarchical knowledge paths      │
│    • Cross-referenced summaries        │
│    • Intelligent categorization        │
├─────────────────────────────────────────┤
│            L2: Raw Archives             │
│    • Complete conversation history     │
│    • Full document storage             │
│    • Searchable content database       │
└─────────────────────────────────────────┘
```

### Directory Structure

```
workspace/
├── .yumem/                 # Configuration
├── l0/current/            # Core user information
│   ├── traits.json        # Personality, skills, philosophy
│   ├── agenda.json        # Current focus and priorities
│   └── meta.json          # System metadata
├── l1/                    # Semantic index
│   ├── index.json         # Node index
│   └── nodes/             # Individual semantic nodes
├── l2/                    # Raw content archive
│   ├── index.json         # Content index
│   └── content/           # Raw files and conversations
├── versions/              # Version history
├── prompts/templates/     # Prompt templates
└── logs/                  # System logs
```

## 🔧 Configuration

### Config File (~/.yumem.yaml)

```yaml
workspace: ~/yumem-workspace
ports:
  mcp: 8080
  web: 3000
memory:
  l0_max_size_kb: 10
  l1_max_nodes: 10000
  l2_max_entries: 100000
import:
  auto_categorize: true
  extract_keywords: true
logging:
  level: info
  file: workspace/logs/yumem.log
```

## 🔌 Integration Examples

### Claude Desktop MCP Integration

Add to your Claude Desktop MCP configuration:

```json
{
  "mcpServers": {
    "yumem": {
      "command": "/path/to/yumem",
      "args": ["--mcp-port", "8080", "--no-browser"],
      "env": {
        "YUMEM_WORKSPACE": "/path/to/workspace"
      }
    }
  }
}
```

### Python Integration

```python
import requests

class YuMemClient:
    def __init__(self, base_url="http://localhost:8080"):
        self.base_url = base_url
    
    def get_context(self):
        response = requests.post(f"{self.base_url}/mcp", json={
            "method": "get_l0_context",
            "params": {}
        })
        return response.json()["data"]
    
    def search_memory(self, query):
        response = requests.post(f"{self.base_url}/mcp", json={
            "method": "search_l1",
            "params": {"query": query}
        })
        return response.json()["data"]

# Usage
memory = YuMemClient()
context = memory.get_context()
results = memory.search_memory("machine learning projects")
```

## 📊 Performance & Scaling

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

## 🛡️ Security & Privacy

### Data Protection
- **Local Storage Only**: Data never leaves your device
- **File Permissions**: Restricted access to workspace directory
- **No Telemetry**: Zero external communication
- **Audit Logs**: Complete operation tracking

### Privacy Features
- **User Ownership**: You control all data
- **Transparent Operations**: All actions are logged and explainable  
- **Data Portability**: Standard formats for easy migration
- **Selective Sharing**: Choose what to share, if anything

## 🤝 Contributing

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
- 🌍 **Internationalization**: Multi-language support
- 🔌 **Integrations**: More AI model and platform integrations  
- 📊 **Analytics**: Advanced memory usage analytics
- 🎨 **UI/UX**: Dashboard improvements and themes
- 🔧 **Importers**: Support for more data sources

## 📚 Documentation

### Deep Dive Documentation
- [**Design Document**](docs/DESIGN.md): Complete architecture and implementation details
- [**Principles & Philosophy**](docs/PRINCIPLES.md): Project vision and design principles

## 🏆 Why YuMem?

### The Problem
Today's AI conversations are ephemeral. Each interaction starts from scratch, lacking the rich context that makes human relationships meaningful. AI assistants can't remember your preferences, learn from past conversations, or build on previous insights.

### The Solution  
YuMem gives AI systems structured, persistent memory that grows with each interaction. It's like giving your AI assistant a brain that remembers, learns, and evolves—while keeping all your data private and under your control.

### The Vision
We're building the foundation for AI relationships that feel as natural and contextual as human ones. Your AI assistant should know your working style, remember your goals, understand your communication preferences, and grow alongside you over time.

## 💝 The Story Behind YuMem

YuMem gets its name from "小愚" (Xiao Yu) - the creator's son's name. Just as a parent remembers every detail of their child's growth, development, and personality, YuMem enables AI to develop that same deep, persistent understanding of you.

The name embodies the project's core philosophy: memory is not just data storage, but the foundation of meaningful relationships and understanding.

## 📄 License

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

**Share your story**: How has persistent AI memory changed your workflow? [#YuMem](https://twitter.com/search?q=%23YuMem) [#LocalFirst](https://twitter.com/search?q=%23LocalFirst) [#AIMemory](https://twitter.com/search?q=%23AIMemory)