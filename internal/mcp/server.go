package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
	"yumem/internal/workspace"
)

type Server struct {
	port            int
	server          *http.Server
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
	retrievalEngine *retrieval.RetrievalEngine
}

func NewServer(port int, l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, retrievalEngine *retrieval.RetrievalEngine) *Server {
	return &Server{
		port:            port,
		l0Manager:       l0Manager,
		l1Manager:       l1Manager,
		l2Manager:       l2Manager,
		promptManager:   promptManager,
		retrievalEngine: retrievalEngine,
	}
}

type MCPRequest struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

type MCPResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (s *Server) Start() error {
	// Ensure workspace is initialized
	if err := workspace.EnsureInitialized(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/health", s.handleHealth)
	
	// Add new endpoints
	mux.HandleFunc("/mcp/get_schema", s.handleGetSchema)
	mux.HandleFunc("/mcp/retrieve_context", s.handleRetrieveContext)

	s.server = &http.Server{
		Addr:    ":" + strconv.Itoa(s.port),
		Handler: mux,
	}

	fmt.Printf("MCP Server started on port %d\n", s.port)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON request")
		return
	}

	response := s.processMCPRequest(req)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	schema := map[string]interface{}{
		"l0_structure": map[string]interface{}{
			"description": "Core user information included in every conversation",
			"categories": []string{"long_term_traits", "recent_agenda"},
			"examples": []string{"personal preferences", "work background", "recent interests"},
		},
		"l1_structure": map[string]interface{}{
			"description": "Semantic tree structure with LLM-generated summaries and indexes",
			"current_paths": []string{"personal/interests", "work/projects", "learning/topics"},
			"format": "path/to/topic",
		},
		"storage_guidelines": map[string]interface{}{
			"l0_criteria": "Long-term stable core user information",
			"l1_criteria": "Categorizable topic information requiring paths and summaries",
			"l2_criteria": "Raw conversation records and complete documents",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{Success: true, Data: schema})
}

func (s *Server) handleRetrieveContext(w http.ResponseWriter, r *http.Request) {
	var req retrieval.ContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{Success: false, Error: "Invalid request format"})
		return
	}

	response, err := s.retrievalEngine.RetrieveContext(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{Success: false, Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{Success: true, Data: response})
}

func (s *Server) processMCPRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "get_l0_context":
		return s.getL0Context()
	case "update_l0":
		return s.updateL0(req.Params)
	case "search_l1":
		return s.searchL1(req.Params)
	case "create_l1_node":
		return s.createL1Node(req.Params)
	case "update_l1_node":
		return s.updateL1Node(req.Params)
	case "search_l2":
		return s.searchL2(req.Params)
	case "add_l2_file":
		return s.addL2File(req.Params)
	case "get_l2_content":
		return s.getL2Content(req.Params)
	default:
		return MCPResponse{Success: false, Error: fmt.Sprintf("Unknown method: %s", req.Method)}
	}
}

func (s *Server) getL0Context() MCPResponse {
	context, err := s.l0Manager.GetContext()
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: context}
}

func (s *Server) updateL0(params map[string]interface{}) MCPResponse {
	userID, _ := params["user_id"].(string)
	name, _ := params["name"].(string)
	context, _ := params["context"].(string)
	
	var preferences map[string]string
	if prefs, ok := params["preferences"].(map[string]interface{}); ok {
		preferences = make(map[string]string)
		for k, v := range prefs {
			if str, ok := v.(string); ok {
				preferences[k] = str
			}
		}
	}

	err := s.l0Manager.Update(userID, name, context, preferences)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true}
}

func (s *Server) searchL1(params map[string]interface{}) MCPResponse {
	query, _ := params["query"].(string)
	
	nodes, err := s.l1Manager.SearchNodes(query)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: nodes}
}

func (s *Server) createL1Node(params map[string]interface{}) MCPResponse {
	path, _ := params["path"].(string)
	title, _ := params["title"].(string)
	summary, _ := params["summary"].(string)
	
	var keywords []string
	if kws, ok := params["keywords"].([]interface{}); ok {
		for _, kw := range kws {
			if str, ok := kw.(string); ok {
				keywords = append(keywords, str)
			}
		}
	}

	var l2Refs []string
	if refs, ok := params["l2_refs"].([]interface{}); ok {
		for _, ref := range refs {
			if str, ok := ref.(string); ok {
				l2Refs = append(l2Refs, str)
			}
		}
	}

	node, err := s.l1Manager.CreateNode(path, title, summary, keywords, l2Refs)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: node}
}

func (s *Server) updateL1Node(params map[string]interface{}) MCPResponse {
	id, _ := params["id"].(string)
	summary, _ := params["summary"].(string)
	
	var keywords []string
	if kws, ok := params["keywords"].([]interface{}); ok {
		for _, kw := range kws {
			if str, ok := kw.(string); ok {
				keywords = append(keywords, str)
			}
		}
	}

	err := s.l1Manager.UpdateNode(id, summary, keywords)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true}
}

func (s *Server) searchL2(params map[string]interface{}) MCPResponse {
	query, _ := params["query"].(string)
	
	var tags []string
	if tagList, ok := params["tags"].([]interface{}); ok {
		for _, tag := range tagList {
			if str, ok := tag.(string); ok {
				tags = append(tags, str)
			}
		}
	}

	entries, err := s.l2Manager.SearchEntries(query, tags)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: entries}
}

func (s *Server) addL2File(params map[string]interface{}) MCPResponse {
	filePath, _ := params["file_path"].(string)
	
	var tags []string
	if tagList, ok := params["tags"].([]interface{}); ok {
		for _, tag := range tagList {
			if str, ok := tag.(string); ok {
				tags = append(tags, str)
			}
		}
	}

	entry, err := s.l2Manager.AddFile(filePath, tags)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: entry}
}

func (s *Server) getL2Content(params map[string]interface{}) MCPResponse {
	id, _ := params["id"].(string)
	
	content, err := s.l2Manager.GetContent(id)
	if err != nil {
		return MCPResponse{Success: false, Error: err.Error()}
	}
	return MCPResponse{Success: true, Data: string(content)}
}

func (s *Server) sendError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(MCPResponse{Success: false, Error: message})
}