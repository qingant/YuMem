package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"yumem/internal/ai"
	"yumem/internal/importers"
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
	if s.aiManager == nil {
		return mcp.NewToolResultError("AI manager not configured, cannot run consolidation"), nil
	}

	result, err := importers.ConsolidateL0(s.l0Manager, s.promptManager, s.aiManager)
	if err != nil {
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
	keywords, err := req.RequireStringSlice("keywords")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
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
