package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"yumem/internal/ai"
	"yumem/internal/importers"
	"yumem/internal/logging"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
	"yumem/internal/workspace"
)

type Server struct {
	port            int
	mcpServer       *server.MCPServer
	sseServer       *server.SSEServer
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
	aiManager       *ai.Manager
	retrievalEngine *retrieval.RetrievalEngine
}

func NewServer(port int, l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager, retrievalEngine *retrieval.RetrievalEngine) *Server {
	return &Server{
		port:            port,
		l0Manager:       l0Manager,
		l1Manager:       l1Manager,
		l2Manager:       l2Manager,
		promptManager:   promptManager,
		aiManager:       aiManager,
		retrievalEngine: retrievalEngine,
	}
}

func (s *Server) Start() error {
	if err := workspace.EnsureInitialized(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	s.mcpServer = server.NewMCPServer(
		"yumem",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	s.registerTools()

	s.sseServer = server.NewSSEServer(s.mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://localhost:%d", s.port)),
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)

	addr := ":" + strconv.Itoa(s.port)
	fmt.Printf("MCP SSE Server started on port %d (SSE: /sse, Message: /message)\n", s.port)
	return s.sseServer.Start(addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.sseServer != nil {
		return s.sseServer.Shutdown(ctx)
	}
	return nil
}

// ServeStdio starts the MCP server over stdio transport (for Claude Desktop integration).
func (s *Server) ServeStdio(ctx context.Context) error {
	if err := workspace.EnsureInitialized(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	s.mcpServer = server.NewMCPServer(
		"yumem",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	s.registerTools()

	stdioServer := server.NewStdioServer(s.mcpServer)
	return stdioServer.Listen(ctx, nil, nil)
}

func (s *Server) registerTools() {
	// 1. get_l0_context
	s.mcpServer.AddTool(
		mcp.NewTool("get_l0_context",
			mcp.WithDescription("Get the user's L0 profile context including traits and agenda"),
		),
		s.handleGetL0Context,
	)

	// 2. update_l0
	s.mcpServer.AddTool(
		mcp.NewTool("update_l0",
			mcp.WithDescription("Update the user's L0 profile (identity, name, context, preferences)"),
			mcp.WithString("user_id", mcp.Description("User ID")),
			mcp.WithString("name", mcp.Description("User display name")),
			mcp.WithString("context", mcp.Description("User context information")),
			mcp.WithString("preferences_json", mcp.Description("JSON string of preference key-value pairs")),
		),
		s.handleUpdateL0,
	)

	// 3. search_l1
	s.mcpServer.AddTool(
		mcp.NewTool("search_l1",
			mcp.WithDescription("Search the L1 semantic index for relevant knowledge nodes"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		),
		s.handleSearchL1,
	)

	// 4. create_l1_node
	s.mcpServer.AddTool(
		mcp.NewTool("create_l1_node",
			mcp.WithDescription("Create a new node in the L1 semantic index tree"),
			mcp.WithString("path", mcp.Required(), mcp.Description("Tree path like 'work/projects/yumem'")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Human-readable title")),
			mcp.WithString("summary", mcp.Description("Summary of the node content")),
			mcp.WithArray("keywords", mcp.Description("Keywords for search"), mcp.WithStringItems()),
			mcp.WithArray("l2_refs", mcp.Description("References to L2 entry IDs"), mcp.WithStringItems()),
		),
		s.handleCreateL1Node,
	)

	// 5. update_l1_node
	s.mcpServer.AddTool(
		mcp.NewTool("update_l1_node",
			mcp.WithDescription("Update an existing L1 node's summary and keywords"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Node ID to update")),
			mcp.WithString("summary", mcp.Description("Updated summary")),
			mcp.WithArray("keywords", mcp.Description("Updated keywords"), mcp.WithStringItems()),
		),
		s.handleUpdateL1Node,
	)

	// 6. search_l2
	s.mcpServer.AddTool(
		mcp.NewTool("search_l2",
			mcp.WithDescription("Search L2 raw content archive"),
			mcp.WithString("query", mcp.Description("Search query")),
			mcp.WithArray("tags", mcp.Description("Filter by tags"), mcp.WithStringItems()),
		),
		s.handleSearchL2,
	)

	// 7. add_l2_file
	s.mcpServer.AddTool(
		mcp.NewTool("add_l2_file",
			mcp.WithDescription("Add a file to the L2 raw content archive"),
			mcp.WithString("path", mcp.Required(), mcp.Description("File path to add")),
			mcp.WithArray("tags", mcp.Description("Tags for categorization"), mcp.WithStringItems()),
		),
		s.handleAddL2File,
	)

	// 8. get_l2_content
	s.mcpServer.AddTool(
		mcp.NewTool("get_l2_content",
			mcp.WithDescription("Get the actual content of an L2 entry by ID"),
			mcp.WithString("id", mcp.Required(), mcp.Description("L2 entry ID")),
		),
		s.handleGetL2Content,
	)

	// 9. consolidate_l0
	s.mcpServer.AddTool(
		mcp.NewTool("consolidate_l0",
			mcp.WithDescription("Consolidate L0 data: deduplicate traits, narrativize values, cap agenda at 10 items"),
		),
		s.handleConsolidateL0,
	)

	// 10. retrieve_context
	s.mcpServer.AddTool(
		mcp.NewTool("retrieve_context",
			mcp.WithDescription("Intelligently retrieve assembled context from all memory layers"),
			mcp.WithArray("keywords", mcp.Required(), mcp.Description("Keywords to search for"), mcp.WithStringItems()),
			mcp.WithArray("scope", mcp.Description("Layers to search: l0, l1, l2 (default: all)"), mcp.WithStringItems()),
			mcp.WithNumber("max_items", mcp.Description("Maximum items to return (default: 10)")),
			mcp.WithBoolean("include_l0", mcp.Description("Include L0 structured context (default: true)")),
			mcp.WithBoolean("summarize", mcp.Description("Use AI to summarize results (default: false)")),
			mcp.WithString("target_length", mcp.Description("Target length: brief, detailed, comprehensive (default: detailed)")),
		),
		s.handleRetrieveContext,
	)

	// === High-level chatbot tools ===

	// 11. store_memory
	s.mcpServer.AddTool(
		mcp.NewTool("store_memory",
			mcp.WithDescription("Store a memory: conversation turn (with session_id) or standalone note. For conversations, content is appended to the same L2 entry per session. Analysis runs on end_session or every 10 turns."),
			mcp.WithString("content", mcp.Required(), mcp.Description("The content to store")),
			mcp.WithString("role", mcp.Description("Role: 'user' or 'assistant' (for conversation mode)")),
			mcp.WithString("session_id", mcp.Description("Session ID to group multi-turn conversation into one L2 entry")),
			mcp.WithString("source", mcp.Description("Source identifier (default: 'mcp')")),
			mcp.WithBoolean("end_session", mcp.Description("Mark session end and trigger analysis (default: false)")),
		),
		s.handleStoreMemory,
	)

	// 12. recall_memory
	s.mcpServer.AddTool(
		mcp.NewTool("recall_memory",
			mcp.WithDescription("Recall relevant memories using AI semantic search on the knowledge tree. Always includes user profile."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language query describing what to recall")),
			mcp.WithNumber("max_topics", mcp.Description("Maximum number of topics to return (default: 5)")),
		),
		s.handleRecallMemory,
	)

	// 13. get_core_memory
	s.mcpServer.AddTool(
		mcp.NewTool("get_core_memory",
			mcp.WithDescription("Get the user's core memory: identity traits, current focus, and preferences. Call this at the start of every conversation to understand who you're talking to."),
		),
		s.handleGetCoreMemory,
	)
}

// Tool handlers

func (s *Server) handleGetL0Context(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := s.l0Manager.GetContext()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(ctx), nil
}

func (s *Server) handleUpdateL0(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := req.GetString("user_id", "")
	name := req.GetString("name", "")
	ctx := req.GetString("context", "")
	prefsJSON := req.GetString("preferences_json", "")

	var preferences map[string]string
	if prefsJSON != "" {
		if err := json.Unmarshal([]byte(prefsJSON), &preferences); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid preferences_json: %v", err)), nil
		}
	}

	if err := s.l0Manager.Update(userID, name, ctx, preferences); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("L0 updated successfully"), nil
}

func (s *Server) handleSearchL1(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	nodes, err := s.l1Manager.SearchNodes(query)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := mcp.NewToolResultJSON(nodes)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleCreateL1Node(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	summary := req.GetString("summary", "")
	keywords := req.GetStringSlice("keywords", nil)
	l2Refs := req.GetStringSlice("l2_refs", nil)

	node, err := s.l1Manager.CreateNode(path, title, summary, keywords, l2Refs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := mcp.NewToolResultJSON(node)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleUpdateL1Node(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	summary := req.GetString("summary", "")
	keywords := req.GetStringSlice("keywords", nil)

	if err := s.l1Manager.UpdateNode(id, summary, keywords); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("L1 node updated successfully"), nil
}

func (s *Server) handleSearchL2(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	tags := req.GetStringSlice("tags", nil)

	entries, err := s.l2Manager.SearchEntries(query, tags)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := mcp.NewToolResultJSON(entries)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleAddL2File(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filePath, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	tags := req.GetStringSlice("tags", nil)

	entry, err := s.l2Manager.AddFile(filePath, tags)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := mcp.NewToolResultJSON(entry)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleGetL2Content(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	content, err := s.l2Manager.GetContent(id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(content)), nil
}

func (s *Server) handleConsolidateL0(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := logging.Get()
	log.Info("mcp", "tool: consolidate_l0")

	if s.aiManager == nil {
		return mcp.NewToolResultError("AI manager not configured, cannot run consolidation"), nil
	}

	result, err := importers.ConsolidateL0(s.l0Manager, s.promptManager, s.aiManager)
	if err != nil {
		log.Error("mcp", fmt.Sprintf("consolidate_l0 failed: %v", err))
		return mcp.NewToolResultError(fmt.Sprintf("consolidation failed: %v", err)), nil
	}

	summary := fmt.Sprintf("Consolidation complete: traits %d→%d, agenda %d→%d",
		result.TraitsBefore, result.TraitsAfter, result.AgendaBefore, result.AgendaAfter)
	if result.ChangesSummary != "" {
		summary += "\n\nChanges: " + result.ChangesSummary
	}
	return mcp.NewToolResultText(summary), nil
}

func (s *Server) handleRetrieveContext(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := logging.Get()
	keywords, err := req.RequireStringSlice("keywords")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	log.Info("mcp", fmt.Sprintf("tool: retrieve_context (keywords=%v)", keywords))
	scope := req.GetStringSlice("scope", nil)
	maxItems := req.GetInt("max_items", 10)
	includeL0 := req.GetBool("include_l0", true)
	summarize := req.GetBool("summarize", false)
	targetLength := req.GetString("target_length", "detailed")

	ctxReq := retrieval.ContextRequest{}
	ctxReq.Query.Keywords = keywords
	ctxReq.Query.Scope = scope
	ctxReq.Query.MaxItems = maxItems
	ctxReq.Query.Type = strings.Join(keywords, " ")
	ctxReq.Requirements.IncludeL0Structure = includeL0
	ctxReq.Requirements.Summarize = summarize
	ctxReq.Requirements.TargetLength = targetLength

	response, err := s.retrievalEngine.RetrieveContext(ctxReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

// === High-level chatbot tool handlers ===

func (s *Server) handleStoreMemory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := logging.Get()
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	role := req.GetString("role", "user")
	sessionID := req.GetString("session_id", "")
	source := req.GetString("source", "mcp")
	endSession := req.GetBool("end_session", false)

	log.Info("mcp", fmt.Sprintf("tool: store_memory (len=%d, session=%s)", len(content), sessionID))

	if sessionID != "" {
		// Conversation mode: append to existing or create new session entry
		return s.storeConversationTurn(content, role, sessionID, source, endSession)
	}

	// Standalone note mode: create L2 + immediate analysis
	return s.storeStandaloneNote(content, source)
}

func (s *Server) storeConversationTurn(content, role, sessionID, source string, endSession bool) (*mcp.CallToolResult, error) {
	now := time.Now()
	timestamp := now.Format("2006-01-02 15:04:05")

	// Format as Markdown conversation turn
	roleLabel := "**User**"
	if role == "assistant" {
		roleLabel = "**Assistant**"
	}
	formattedContent := fmt.Sprintf("#### %s — %s\n\n%s\n\n---\n\n", roleLabel, timestamp, content)

	// Look for existing session
	existingEntry, err := s.l2Manager.FindByMetadata("session_id", sessionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search for session: %v", err)), nil
	}

	var l2ID string
	var turnCount int

	if existingEntry != nil {
		// Append to existing session
		if err := s.l2Manager.AppendContent(existingEntry.ID, formattedContent); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to append content: %v", err)), nil
		}
		l2ID = existingEntry.ID

		// Parse and increment turn count
		if tc, ok := existingEntry.Metadata["turn_count"]; ok {
			fmt.Sscanf(tc, "%d", &turnCount)
		}
		turnCount++

		// Update metadata
		if err := s.l2Manager.UpdateMetadata(l2ID, map[string]string{
			"turn_count":   fmt.Sprintf("%d", turnCount),
			"last_role":    role,
			"updated_at":   now.Format(time.RFC3339),
		}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update metadata: %v", err)), nil
		}
	} else {
		// Create new session entry with Markdown header
		title := fmt.Sprintf("conversation_%s", sessionID)
		header := fmt.Sprintf("# Conversation %s\n\n**Source**: %s  \n**Started**: %s\n\n---\n\n", sessionID, source, timestamp)
		initialContent := header + formattedContent

		l2Tags := []string{"conversation", source}
		entry, err := s.l2Manager.AddEntry(title, initialContent, "conversation", source, l2Tags)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create session entry: %v", err)), nil
		}
		l2ID = entry.ID
		turnCount = 1

		// Set comprehensive session metadata
		if err := s.l2Manager.UpdateMetadata(l2ID, map[string]string{
			"session_id":   sessionID,
			"content_type": "conversation",
			"source":       source,
			"turn_count":   "1",
			"first_role":   role,
			"last_role":    role,
			"started_at":   now.Format(time.RFC3339),
			"updated_at":   now.Format(time.RFC3339),
		}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to set session metadata: %v", err)), nil
		}
	}

	// Trigger analysis on end_session or every 10 turns
	shouldAnalyze := endSession || (turnCount > 0 && turnCount%10 == 0)

	response := map[string]interface{}{
		"status":     "stored",
		"l2_id":      l2ID,
		"session_id": sessionID,
		"turn_count": turnCount,
		"analyzed":   false,
	}

	if shouldAnalyze && s.aiManager != nil {
		// Run analysis in background goroutine
		go func() {
			bi := importers.NewBaseImporter(s.l0Manager, s.l1Manager, s.l2Manager, s.promptManager, s.aiManager)
			contentBytes, err := s.l2Manager.GetContent(l2ID)
			if err != nil {
				fmt.Printf("  ⚠️  Failed to read session content for analysis: %v\n", err)
				return
			}
			title := fmt.Sprintf("conversation_%s", sessionID)
			bi.AnalyzeAndApply(l2ID, title, string(contentBytes), "conversation", time.Time{}, nil)
		}()
		response["analyzed"] = true
		response["analysis_note"] = "Analysis triggered in background"
	}

	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) storeStandaloneNote(content, source string) (*mcp.CallToolResult, error) {
	now := time.Now()
	title := fmt.Sprintf("note_%s", now.Format("20060102_150405"))
	l2Tags := []string{"note", source}
	entry, err := s.l2Manager.AddEntry(title, content, "note", source, l2Tags)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to store note: %v", err)), nil
	}

	// Set rich metadata
	if err := s.l2Manager.UpdateMetadata(entry.ID, map[string]string{
		"content_type": "note",
		"source":       source,
		"created_at":   now.Format(time.RFC3339),
	}); err != nil {
		// Non-fatal, metadata is supplementary
		fmt.Printf("  ⚠️  Failed to set note metadata: %v\n", err)
	}

	response := map[string]interface{}{
		"status":   "stored",
		"l2_id":    entry.ID,
		"analyzed": false,
	}

	// Run analysis immediately if AI is available
	if s.aiManager != nil {
		bi := importers.NewBaseImporter(s.l0Manager, s.l1Manager, s.l2Manager, s.promptManager, s.aiManager)
		if err := bi.AnalyzeAndApply(entry.ID, title, content, source, time.Time{}, nil); err != nil {
			response["analysis_error"] = err.Error()
		} else {
			response["analyzed"] = true
		}
	}

	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleRecallMemory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := logging.Get()
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxTopics := req.GetInt("max_topics", 5)

	log.Info("mcp", fmt.Sprintf("tool: recall_memory (query=%q, max=%d)", query, maxTopics))

	response, err := s.retrievalEngine.RecallMemory(query, maxTopics)
	if err != nil {
		log.Error("mcp", fmt.Sprintf("recall_memory failed: %v", err))
		return mcp.NewToolResultError(fmt.Sprintf("recall failed: %v", err)), nil
	}

	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

func (s *Server) handleGetCoreMemory(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := logging.Get()
	log.Info("mcp", "tool: get_core_memory")

	coreMemory, err := s.retrievalEngine.GetCoreMemory()
	if err != nil {
		log.Error("mcp", fmt.Sprintf("get_core_memory failed: %v", err))
		return mcp.NewToolResultError(fmt.Sprintf("failed to get core memory: %v", err)), nil
	}
	return mcp.NewToolResultText(coreMemory), nil
}
