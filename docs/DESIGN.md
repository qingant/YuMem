# YuMem System Design Documentation

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Memory Layer Design](#memory-layer-design)
4. [Core Components](#core-components)
5. [Data Flow](#data-flow)
6. [API Design](#api-design)
7. [Storage Implementation](#storage-implementation)
8. [Versioning Strategy](#versioning-strategy)
9. [Context Retrieval Engine](#context-retrieval-engine)
10. [Web Dashboard](#web-dashboard)
11. [Import System](#import-system)
12. [Security Considerations](#security-considerations)

## Overview

YuMem is a comprehensive memory management system designed to enable AI models to understand users as deeply as they understand themselves. The system maintains a structured, persistent memory across conversations, allowing AI assistants to provide increasingly personalized and contextually relevant interactions.

### Core Objectives
- **Persistent Memory**: Maintain long-term user context across sessions
- **Structured Intelligence**: Organize information in semantically meaningful ways
- **Privacy-First**: Local storage with user-controlled data
- **AI Integration**: Seamless integration with AI models via MCP protocol
- **Scalable Architecture**: Efficient storage and retrieval at any scale

## Architecture

YuMem implements a three-layer memory architecture inspired by human memory systems:

```
┌─────────────────────────────────────────┐
│                YuMem CLI                │
│          (Command Interface)            │
├─────────────────────────────────────────┤
│            Web Dashboard                │
│         (Management UI)                 │
├─────────────────────────────────────────┤
│             MCP Server                  │
│         (AI Integration)                │
├─────────────────────────────────────────┤
│        Context Retrieval Engine         │
│      (Intelligent Information           │
│       Assembly & Ranking)               │
├─────────────────────────────────────────┤
│  L0 Manager │  L1 Manager │ L2 Manager  │
│ (Core Info) │ (Semantic   │ (Raw Text)  │
│             │  Index)     │             │
├─────────────────────────────────────────┤
│        Versioning & Prompt System       │
├─────────────────────────────────────────┤
│            File System Storage          │
└─────────────────────────────────────────┘
```

## Memory Layer Design

### L0 Layer: Core User Information
**Purpose**: Store essential, frequently-accessed user information that should be included in every conversation.

**Structure**:
```go
type L0Data struct {
    UserID string
    
    LongTermTraits struct {
        Personality map[string]TimestampedValue
        Philosophy  map[string]TimestampedValue
        Background  map[string]TimestampedValue
        Skills      map[string]TimestampedValue
    }
    
    RecentAgenda struct {
        CurrentFocus   []AgendaItem
        CompletedItems []AgendaItem
        OnHoldItems    []AgendaItem
    }
    
    Meta struct {
        Version       string
        LastUpdated   time.Time
        SizeBytes     int64
        UpdateTrigger string
    }
}
```

**Key Features**:
- **Size-Controlled**: Maximum 10KB to ensure fast loading
- **Subcategory Storage**: Traits, agenda, and metadata in separate files
- **Versioned Updates**: All changes tracked with timestamps
- **Rich Metadata**: Source attribution and confidence scoring

### L1 Layer: Semantic Index Tree
**Purpose**: Hierarchical organization of knowledge with semantic paths and summaries.

**Structure**:
```go
type L1Node struct {
    ID          string
    Path        string    // e.g., "work/projects/yumem"
    Title       string
    Summary     string
    Keywords    []string
    L2Refs      []string  // References to L2 entries
    CreatedAt   time.Time
    UpdatedAt   time.Time
    AccessCount int
    LastAccess  time.Time
}
```

**Key Features**:
- **Hierarchical Paths**: Intuitive organization like a file system
- **Semantic Search**: Keyword-based and content-based retrieval
- **L2 References**: Links to supporting raw content
- **Usage Tracking**: Access patterns for intelligent ranking
- **Append-Only**: Preserves complete history

### L2 Layer: Raw Text Index
**Purpose**: Store complete, unprocessed content with metadata for detailed reference.

**Structure**:
```go
type L2Entry struct {
    ID          string
    Title       string
    Content     string
    ContentType string    // "conversation", "document", "note"
    Source      string    // File path, URL, or source identifier
    Tags        []string
    CreatedAt   time.Time
    FileHash    string    // For deduplication
    Size        int64
}
```

**Key Features**:
- **Complete Content**: No summarization or loss of information
- **Rich Metadata**: Tags, source attribution, and content typing
- **Deduplication**: Hash-based duplicate detection
- **Full-Text Search**: Complete content indexing
- **Append-Only**: Historical preservation

## Core Components

### 1. Memory Managers

#### L0Manager
```go
type L0Manager struct {
    dataPath string
}

// Key Methods:
func (m *L0Manager) Load() (*L0Data, error)
func (m *L0Manager) Save(data *L0Data) error
func (m *L0Manager) Update(userID, name, context string, preferences map[string]string) error
func (m *L0Manager) GetContext() (string, error)
```

**Responsibilities**:
- Load/save L0 data from subcategory files
- Size management and overflow handling
- Context string generation for AI models
- Atomic updates with versioning

#### L1Manager
```go
type L1Manager struct {
    indexPath string
    nodesPath string
}

// Key Methods:
func (m *L1Manager) CreateNode(path, title, summary string, keywords, l2Refs []string) (*L1Node, error)
func (m *L1Manager) UpdateNode(id, summary string, keywords []string) error
func (m *L1Manager) SearchNodes(query string) ([]*L1Node, error)
func (m *L1Manager) GetNodesByPath(pathPrefix string) ([]*L1Node, error)
```

**Responsibilities**:
- Hierarchical path management
- Semantic search and ranking
- L2 reference maintenance
- Usage analytics and optimization

#### L2Manager
```go
type L2Manager struct {
    indexPath   string
    contentPath string
}

// Key Methods:
func (m *L2Manager) AddEntry(title, content, contentType, source string, tags []string) (*L2Entry, error)
func (m *L2Manager) AddFile(filePath string, tags []string) (*L2Entry, error)
func (m *L2Manager) SearchEntries(query string, tags []string) ([]*L2Entry, error)
func (m *L2Manager) GetContent(id string) ([]byte, error)
```

**Responsibilities**:
- Content storage and retrieval
- File import and processing
- Full-text search capabilities
- Deduplication and optimization

### 2. Context Retrieval Engine

The retrieval engine intelligently assembles context for AI conversations:

```go
type RetrievalEngine struct {
    l0Manager       *L0Manager
    l1Manager       *L1Manager
    l2Manager       *L2Manager
    promptManager   *prompts.PromptManager
}

type ContextRequest struct {
    Query           string
    MaxTokens       int
    IncludeL0       bool
    L1SearchDepth   int
    L2ContentLimit  int
    PreferRecent    bool
    LanguageHint    string
}

type ContextResponse struct {
    L0Context       string
    L1Nodes         []*L1Node
    L2Excerpts      []L2Excerpt
    TotalTokens     int
    RetrievalTime   time.Duration
    Sources         []string
}
```

**Algorithm**:
1. **L0 Assembly**: Always include core user information
2. **Query Analysis**: Extract keywords and intent
3. **L1 Search**: Semantic search with ranking
4. **L2 Retrieval**: Fetch relevant content excerpts
5. **Token Management**: Fit within specified limits
6. **Time Weighting**: Prefer recent and frequently accessed content

### 3. Versioning System

```go
type VersionManager struct {
    manifestPath string
}

type Manifest struct {
    Version     string
    Timestamp   time.Time
    L0Version   int
    L1Snapshot  string
    L2Snapshot  string
    Changes     []ChangeRecord
}
```

**Features**:
- **L0 Versioning**: Track changes to core user information
- **L1/L2 Snapshots**: Point-in-time captures for rollback
- **Change Records**: Detailed modification logs
- **Size Management**: Automatic L0 overflow handling

### 4. Prompt Management System

```go
type PromptManager struct {
    promptsDir string
}

type PromptTemplate struct {
    Name        string
    Description string
    Template    string
    Category    string
    Variables   []string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

**Default Templates**:
- **Context Assembly**: Format user information for AI models
- **Data Analysis**: Process imported content
- **Statistics Generation**: System usage reports
- **Memory Optimization**: Suggest L0 improvements

## Data Flow

### 1. Information Storage Flow
```
User Input → CLI/Web Interface → Managers → File System
     ↓
Content Analysis → L1 Node Creation → L2 Reference Links
     ↓
Versioning Update → Manifest Recording
```

### 2. Context Retrieval Flow
```
AI Request → MCP Server → Retrieval Engine
     ↓
L0 Load → L1 Search → L2 Content Fetch
     ↓
Context Assembly → Language Adaptation → Response
```

### 3. Import Processing Flow
```
Data Source → Importer → Content Analysis
     ↓
L1 Node Generation → L2 Entry Creation → Cross-References
     ↓
Deduplication → Version Update
```

## API Design

### MCP Server Endpoints

#### Core Memory Operations
- `POST /mcp` with method `get_l0_context` - Retrieve core user context
- `POST /mcp` with method `update_l0` - Update core information
- `POST /mcp` with method `search_l1` - Search semantic index
- `POST /mcp` with method `create_l1_node` - Create new semantic node
- `POST /mcp` with method `search_l2` - Search raw content

#### Specialized Endpoints
- `GET /mcp/get_schema` - System schema information
- `POST /mcp/retrieve_context` - Intelligent context assembly

#### Import Endpoints
- `POST /mcp/import_notes` - Import Apple Notes
- `POST /mcp/import_filesystem` - Import filesystem content

### Web Dashboard API
- `GET /api/stats` - System statistics
- `GET /api/memory/l0` - L0 dashboard data
- `POST /api/prompts` - Prompt management
- `GET /health` - Health check

## Storage Implementation

### Directory Structure
```
workspace/
├── .yumem/                 # Configuration
├── l0/
│   └── current/
│       ├── traits.json     # Personality, skills, etc.
│       ├── agenda.json     # Current focus items
│       └── meta.json       # Metadata
├── l1/
│   ├── index.json         # Node index
│   └── nodes/             # Individual node files
├── l2/
│   ├── index.json         # Content index
│   └── content/           # Raw content files
├── versions/
│   └── manifests/         # Version history
├── prompts/
│   └── templates/         # Prompt templates
└── logs/                  # System logs
```

### File Format Standards
- **JSON**: All structured data (L0, L1 index, L2 index)
- **Plain Text**: L2 content storage
- **UTF-8**: Universal character encoding
- **Atomic Writes**: Temporary files with rename operations

### Performance Optimizations
- **Lazy Loading**: Load content only when needed
- **Caching**: In-memory caching of frequently accessed data
- **Indexing**: Efficient search data structures
- **Compression**: Optional content compression for large files

## Versioning Strategy

### L0 Versioning
- **Subcategory Tracking**: Independent versioning of traits, agenda, metadata
- **Size Monitoring**: Automatic overflow to L1 when exceeding limits
- **Change Attribution**: Track what triggered each update

### L1/L2 Append-Only
- **Immutable History**: Never delete or modify existing entries
- **Soft Deletion**: Mark entries as deprecated rather than removing
- **Snapshot Points**: Periodic full state captures

### Rollback Capabilities
- **Point-in-Time Recovery**: Restore to any previous state
- **Selective Rollback**: Revert specific components only
- **Change Verification**: Preview changes before applying

## Context Retrieval Engine

### Ranking Algorithm
```go
func calculateRelevanceScore(node *L1Node, query string, timeWeight float64) float64 {
    // Keyword matching score
    keywordScore := calculateKeywordMatch(node.Keywords, query)
    
    // Recency bonus
    timeSince := time.Since(node.LastAccess)
    recencyScore := math.Exp(-timeSince.Hours() / 168) // Weekly decay
    
    // Access frequency bonus
    frequencyScore := math.Log(float64(node.AccessCount + 1))
    
    // Path relevance (deeper paths often more specific)
    pathScore := 1.0 - (float64(strings.Count(node.Path, "/")) * 0.1)
    
    return (keywordScore * 0.4) + 
           (recencyScore * timeWeight * 0.3) + 
           (frequencyScore * 0.2) + 
           (pathScore * 0.1)
}
```

### Token Management
- **Dynamic Allocation**: Distribute tokens based on content importance
- **Hierarchical Truncation**: Remove less important content first
- **Summary Generation**: Create abstracts when content too large
- **Context Optimization**: Prefer diverse, complementary information

## Web Dashboard

### Architecture
- **Backend**: Go HTTP server with JSON APIs
- **Frontend**: Vue.js 3 with Tailwind CSS
- **Templates**: Go HTML templates with embedded assets
- **Real-time Updates**: Server-sent events for live statistics

### Key Features
- **System Monitoring**: Memory usage, API statistics, performance metrics
- **Prompt Management**: CRUD operations for templates
- **Memory Visualization**: Interactive tree views of L1 structure
- **Import Tools**: Batch processing interfaces
- **Configuration**: System settings and preferences

### Security Features
- **Local-Only**: No external network dependencies
- **CORS Protection**: Restrict cross-origin requests
- **Input Validation**: Sanitize all user inputs
- **File Path Protection**: Prevent directory traversal

## Import System

### Base Importer Interface
```go
type Importer interface {
    Import(sources []string, options ImportOptions) (*ImportResult, error)
    Analyze(item *ImportItem) (*ItemAnalysis, error)
    GetSupportedTypes() []string
}
```

### Apple Notes Importer
- **SQLite Integration**: Direct database access
- **Metadata Preservation**: Creation dates, modification times
- **Attachment Handling**: Images, files, links
- **Folder Structure**: Maintain hierarchical organization

### Filesystem Importer
- **Recursive Scanning**: Process directory trees
- **File Type Detection**: Automatic content type identification
- **Deduplication**: Hash-based duplicate prevention
- **Selective Import**: Pattern-based file filtering

### Content Analysis
- **LLM Integration**: Intelligent summarization and categorization
- **Keyword Extraction**: Automatic tag generation
- **Relationship Detection**: Cross-reference identification
- **Quality Scoring**: Content importance assessment

## Security Considerations

### Data Privacy
- **Local Storage**: All data remains on user's device
- **No Telemetry**: No data transmission to external servers
- **Encryption Ready**: Infrastructure for optional encryption
- **User Control**: Complete data ownership and control

### Access Control
- **File Permissions**: Restricted directory access
- **API Authentication**: Optional token-based authentication
- **Network Binding**: Local-only server binding by default
- **Audit Logging**: Track all system operations

### Data Integrity
- **Atomic Operations**: Prevent partial writes
- **Backup Verification**: Validate backup completeness
- **Corruption Detection**: Checksum validation
- **Recovery Procedures**: Automated corruption repair

## Performance Characteristics

### Scalability Targets
- **L0 Size**: < 10KB (fast loading)
- **L1 Nodes**: Up to 10,000 nodes
- **L2 Entries**: Up to 100,000 entries
- **Query Response**: < 100ms for typical requests
- **Memory Usage**: < 100MB RAM footprint

### Optimization Strategies
- **Lazy Loading**: Load data on demand
- **Caching**: Intelligent cache management
- **Indexing**: Efficient search structures
- **Compression**: Space-efficient storage
- **Batch Operations**: Minimize I/O operations

## Future Enhancements

### Planned Features
- **Encryption**: Optional data encryption at rest
- **Sync**: Multi-device synchronization
- **Plugins**: Extensible importer system
- **Analytics**: Advanced usage analytics
- **API Versioning**: Backward-compatible API evolution

### Extensibility Points
- **Custom Importers**: Plugin architecture for new data sources
- **Retrieval Strategies**: Pluggable ranking algorithms
- **Storage Backends**: Alternative storage implementations
- **UI Themes**: Customizable dashboard appearances
- **Language Support**: Internationalization framework

## Conclusion

YuMem represents a comprehensive approach to AI memory management, combining the persistent nature of human memory with the precision of computer systems. Its three-layer architecture provides both immediate accessibility and long-term preservation, while the versioning system ensures data integrity and recoverability.

The system's design prioritizes user privacy, data ownership, and seamless AI integration, making it an ideal foundation for building truly personalized AI assistants that grow and learn with their users over time.