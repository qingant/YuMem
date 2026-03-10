package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/importers"
	"yumem/internal/logging"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
	"yumem/internal/versioning"
	"gopkg.in/yaml.v2"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*
var templateFiles embed.FS

type DashboardServer struct {
	port            int
	server          *http.Server
	startTime       time.Time
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
	versionManager  *versioning.VersionManager
	retrievalEngine *retrieval.RetrievalEngine
	aiManager       *ai.Manager
}

type SystemStats struct {
	MemoryStats struct {
		L0Size       string `json:"l0_size"`
		L0CurrentKB  float64 `json:"l0_current_kb"`
		L0MaxKB      int     `json:"l0_max_kb"`
		L0Usage      float64 `json:"l0_usage_percent"`
		L1NodeCount  int     `json:"l1_node_count"`
		L2EntryCount int     `json:"l2_entry_count"`
		TotalSize    string  `json:"total_size"`
	} `json:"memory_stats"`
	
	UsageStats struct {
		MCPRequests    int    `json:"mcp_requests"`
		RetrievalCalls int    `json:"retrieval_calls"`
		StorageOps     int    `json:"storage_ops"`
		UpTime         string `json:"uptime"`
	} `json:"usage_stats"`
	
	PromptStats struct {
		TotalTemplates   int    `json:"total_templates"`
		RecentlyUpdated  int    `json:"recently_updated"`
		MostUsedTemplate string `json:"most_used_template"`
	} `json:"prompt_stats"`
}

func NewDashboardServer(port int, l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, versionManager *versioning.VersionManager, retrievalEngine *retrieval.RetrievalEngine, aiManager *ai.Manager) *DashboardServer {
	return &DashboardServer{
		port:            port,
		startTime:       time.Now(),
		l0Manager:       l0Manager,
		l1Manager:       l1Manager,
		l2Manager:       l2Manager,
		promptManager:   promptManager,
		versionManager:  versionManager,
		retrievalEngine: retrievalEngine,
		aiManager:       aiManager,
	}
}

func (ds *DashboardServer) Start() error {
	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	// Pages
	mux.HandleFunc("/", ds.handleDashboard)
	mux.HandleFunc("/prompts", ds.handlePrompts)
	mux.HandleFunc("/memory", ds.handleMemory)
	mux.HandleFunc("/stats", ds.handleStats)
	mux.HandleFunc("/settings", ds.handleSettings)
	mux.HandleFunc("/ai-config", ds.handleAIConfigPage)
	mux.HandleFunc("/tools", ds.handleToolsPage)
	mux.HandleFunc("/logs", ds.handleLogsPage)

	// API endpoints
	mux.HandleFunc("/api/stats", ds.handleAPIStats)
	mux.HandleFunc("/api/prompts", ds.handleAPIPrompts)
	mux.HandleFunc("/api/prompts/test", ds.handleAPIPromptTest)
	mux.HandleFunc("/api/memory/l0", ds.handleAPIL0)
	mux.HandleFunc("/api/memory/l1", ds.handleAPIL1)
	mux.HandleFunc("/api/memory/l2", ds.handleAPIL2)
	mux.HandleFunc("/api/memory/l2/content", ds.handleAPIL2Content)
	mux.HandleFunc("/api/memory/l1/search", ds.handleAPIL1Search)
	mux.HandleFunc("/api/memory/l2/search", ds.handleAPIL2Search)
	mux.HandleFunc("/api/memory/l2/conversation", ds.handleAPIL2Conversation)
	mux.HandleFunc("/api/config", ds.handleAPIConfig)
	mux.HandleFunc("/api/version", ds.handleAPIVersion)
	mux.HandleFunc("/api/ai/config", ds.handleAIConfig)
	mux.HandleFunc("/api/ai/providers", ds.handleAIProviders)
	mux.HandleFunc("/api/ai/test", ds.handleAITest)
	mux.HandleFunc("/api/ai/usage", ds.handleAIUsage)
	mux.HandleFunc("/api/ai/usage/reset", ds.handleAIUsageReset)
	mux.HandleFunc("/api/ai/github/auth", ds.handleGitHubAuth)
	mux.HandleFunc("/api/ai/github/status", ds.handleGitHubAuthStatus)
	mux.HandleFunc("/api/ai/github/callback", ds.handleGitHubCallback)

	// Logs API
	mux.HandleFunc("/api/logs", ds.handleAPILogs)

	// Memory tools API
	mux.HandleFunc("/api/tools/core-memory", ds.handleAPICoreMemory)
	mux.HandleFunc("/api/tools/recall-memory", ds.handleAPIRecallMemory)
	mux.HandleFunc("/api/tools/store-memory", ds.handleAPIStoreMemory)

	// Health check
	mux.HandleFunc("/health", ds.handleHealth)

	ds.server = &http.Server{
		Addr:    ":" + strconv.Itoa(ds.port),
		Handler: mux,
	}

	fmt.Printf("Dashboard server starting on port %d\n", ds.port)
	return ds.server.ListenAndServe()
}

func (ds *DashboardServer) Shutdown(ctx context.Context) error {
	if ds.server != nil {
		return ds.server.Shutdown(ctx)
	}
	return nil
}

func (ds *DashboardServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := ds.getSystemStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	versionHistory, _ := ds.versionManager.GetVersionHistory(5)

	ds.renderTemplate(w, "dashboard.html", map[string]interface{}{
		"Title":   "YuMem Dashboard",
		"Page":    "dashboard",
		"Stats":   stats,
		"History": versionHistory,
	})
}

func (ds *DashboardServer) handlePrompts(w http.ResponseWriter, r *http.Request) {
	prompts, err := ds.promptManager.ListPrompts("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	categories, err := ds.promptManager.GetCategories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ds.renderTemplate(w, "prompts.html", map[string]interface{}{
		"Title":      "Prompt Management",
		"Page":       "prompts",
		"Prompts":    prompts,
		"Categories": categories,
	})
}

func (ds *DashboardServer) handleMemory(w http.ResponseWriter, r *http.Request) {
	l0Data, _ := ds.l0Manager.Load()
	l1Tree, _ := ds.l1Manager.GetTree()
	l2Entries, _ := ds.l2Manager.SearchEntries("", []string{})

	ds.renderTemplate(w, "memory.html", map[string]interface{}{
		"Title":     "Memory Management",
		"Page":      "memory",
		"L0Data":    l0Data,
		"L1Tree":    l1Tree,
		"L2Entries": l2Entries,
	})
}

func (ds *DashboardServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := ds.getSystemStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	versionHistory, _ := ds.versionManager.GetVersionHistory(10)

	ds.renderTemplate(w, "stats.html", map[string]interface{}{
		"Title":   "Statistics & Analytics",
		"Page":    "stats",
		"Stats":   stats,
		"History": versionHistory,
	})
}

func (ds *DashboardServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := ds.getSystemStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (ds *DashboardServer) handleAPIPrompts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		category := r.URL.Query().Get("category")
		prompts, err := ds.promptManager.ListPrompts(category)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(prompts)

	case http.MethodPost:
		var prompt prompts.PromptTemplate
		if err := json.NewDecoder(r.Body).Decode(&prompt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := ds.promptManager.SavePrompt(&prompt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}
}

func (ds *DashboardServer) handleAPIPromptTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var testReq struct {
		Category string      `json:"category"`
		Name     string      `json:"name"`
		TestData interface{} `json:"test_data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	prompt, err := ds.promptManager.LoadPrompt(testReq.Category, testReq.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	data := testReq.TestData
	if data == nil {
		data = prompt.TestData
	}

	result, err := ds.promptManager.RenderPrompt(prompt, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"result": result})
}

func (ds *DashboardServer) handleAPIL0(w http.ResponseWriter, r *http.Request) {
	l0Data, err := ds.l0Manager.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(l0Data)
}

func (ds *DashboardServer) handleAPIL1(w http.ResponseWriter, r *http.Request) {
	tree, err := ds.l1Manager.GetTree()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

func (ds *DashboardServer) handleAPIL2(w http.ResponseWriter, r *http.Request) {
	entries, err := ds.l2Manager.SearchEntries("", []string{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (ds *DashboardServer) handleAPIVersion(w http.ResponseWriter, r *http.Request) {
	history, err := ds.versionManager.GetVersionHistory(10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (ds *DashboardServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	cfg, _ := ds.loadConfig()

	ds.renderTemplate(w, "settings.html", map[string]interface{}{
		"Title":  "Settings",
		"Page":   "settings",
		"Config": cfg,
		"Port":   ds.port,
	})
}

func (ds *DashboardServer) handleAPIL2Content(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id parameter required", http.StatusBadRequest)
		return
	}

	content, err := ds.l2Manager.GetContent(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
}

func (ds *DashboardServer) handleAPIL1Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	nodes, err := ds.l1Manager.SearchNodes(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func (ds *DashboardServer) handleAPIL2Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	entries, err := ds.l2Manager.SearchEntries(query, []string{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (ds *DashboardServer) handleAPIL2Conversation(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id parameter required", http.StatusBadRequest)
		return
	}

	entry, err := ds.l2Manager.GetEntry(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if entry.Type != "conversation" {
		http.Error(w, "not a conversation entry", http.StatusBadRequest)
		return
	}

	meta, err := ds.l2Manager.GetConversationMeta(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	messages, err := ds.l2Manager.GetAllMessages(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"meta":     meta,
		"messages": messages,
	})
}

func (ds *DashboardServer) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := ds.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sanitize API keys
	sanitized := map[string]interface{}{
		"workspace_dir": cfg.WorkspaceDir,
		"ai": map[string]interface{}{
			"default_provider": cfg.AI.DefaultProvider,
		},
		"port": ds.port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sanitized)
}

func (ds *DashboardServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(ds.startTime)
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"uptime":    uptime.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (ds *DashboardServer) renderTemplate(w http.ResponseWriter, templateName string, data interface{}) {
	tmpl, err := template.ParseFS(templateFiles, "templates/layout.html", "templates/"+templateName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ds *DashboardServer) getSystemStats() (*SystemStats, error) {
	stats := &SystemStats{}

	// Memory stats - calculate actual L0 size
	l0Data, err := ds.l0Manager.Load()
	if err == nil {
		l0Bytes, marshalErr := json.Marshal(l0Data)
		if marshalErr == nil {
			sizeKB := float64(len(l0Bytes)) / 1024.0
			stats.MemoryStats.L0CurrentKB = sizeKB
		}
		stats.MemoryStats.L0MaxKB = 10
		stats.MemoryStats.L0Usage = (stats.MemoryStats.L0CurrentKB / float64(stats.MemoryStats.L0MaxKB)) * 100
		stats.MemoryStats.L0Size = fmt.Sprintf("%.1fKB", stats.MemoryStats.L0CurrentKB)
	}

	l1Tree, err := ds.l1Manager.GetTree()
	if err == nil {
		stats.MemoryStats.L1NodeCount = len(l1Tree)
	}

	// Get actual L2 count
	l2Entries, err := ds.l2Manager.SearchEntries("", []string{})
	if err == nil {
		stats.MemoryStats.L2EntryCount = len(l2Entries)
		var totalSize int64
		for _, entry := range l2Entries {
			totalSize += entry.Size
		}
		if totalSize > 1024*1024 {
			stats.MemoryStats.TotalSize = fmt.Sprintf("%.1fMB", float64(totalSize)/(1024*1024))
		} else if totalSize > 1024 {
			stats.MemoryStats.TotalSize = fmt.Sprintf("%.1fKB", float64(totalSize)/1024)
		} else {
			stats.MemoryStats.TotalSize = fmt.Sprintf("%dB", totalSize)
		}
	}

	// Usage stats - calculate actual uptime
	uptime := time.Since(ds.startTime)
	if uptime.Hours() >= 1 {
		stats.UsageStats.UpTime = fmt.Sprintf("%.0fh %dm", uptime.Hours(), int(uptime.Minutes())%60)
	} else if uptime.Minutes() >= 1 {
		stats.UsageStats.UpTime = fmt.Sprintf("%.0fm %ds", uptime.Minutes(), int(uptime.Seconds())%60)
	} else {
		stats.UsageStats.UpTime = fmt.Sprintf("%.0fs", uptime.Seconds())
	}

	// Prompt stats
	promptList, err := ds.promptManager.ListPrompts("")
	if err == nil {
		stats.PromptStats.TotalTemplates = len(promptList)
	}

	return stats, nil
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	
	return "127.0.0.1"
}

// AI Configuration Handlers

func (ds *DashboardServer) handleAIConfigPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templateFiles, "templates/layout.html", "templates/ai-config.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Title string
		Page  string
	}{
		Title: "AI Configuration",
		Page:  "ai-config",
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (ds *DashboardServer) handleAIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg, err := ds.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"defaultProvider": cfg.AI.DefaultProvider,
		"defaultModel":    "",
	}

	// Get default model from provider config
	if cfg.AI.DefaultProvider != "" {
		if providerCfg, exists := cfg.AI.Providers[cfg.AI.DefaultProvider]; exists {
			response["defaultModel"] = providerCfg.Model
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ds *DashboardServer) handleAIProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ds.listProviders(w, r)
	case http.MethodPost:
		ds.saveProvider(w, r)
	case http.MethodDelete:
		ds.deleteProvider(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ds *DashboardServer) listProviders(w http.ResponseWriter, r *http.Request) {
	cfg, err := ds.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var providers []map[string]interface{}
	for name, providerCfg := range cfg.AI.Providers {
		provider := map[string]interface{}{
			"name":      name,
			"type":      providerCfg.Type,
			"model":     providerCfg.Model,
			"isDefault": name == cfg.AI.DefaultProvider,
			"hasKey":    providerCfg.APIKey != "",
		}

		// Masked key preview
		if providerCfg.APIKey != "" {
			key := providerCfg.APIKey
			if len(key) > 8 {
				provider["keyPreview"] = key[:4] + "..." + key[len(key)-4:]
			} else {
				provider["keyPreview"] = "****"
			}
		}

		// Add context size info for display
		if providerCfg.Model != "" {
			provider["contextSize"] = ds.getModelContextSize(providerCfg.Type, providerCfg.Model)
		}

		// Add per-provider usage stats if tracker is available
		if ds.aiManager != nil && ds.aiManager.Usage != nil {
			summary := ds.aiManager.Usage.GetSummary(0)
			if pu, ok := summary.ByProvider[name]; ok {
				provider["usage"] = pu
			}
		}

		providers = append(providers, provider)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(providers)
}

func (ds *DashboardServer) saveProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider   string `json:"provider"`
		APIKey     string `json:"apiKey"`
		Model      string `json:"model"`
		BaseURL    string `json:"baseURL"`
		SetDefault bool   `json:"setDefault"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg, err := ds.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if cfg.AI.Providers == nil {
		cfg.AI.Providers = make(map[string]config.ProviderConfig)
	}

	providerCfg := config.ProviderConfig{
		Type:   req.Provider,
		APIKey: req.APIKey,
		Model:  req.Model,
	}

	if req.BaseURL != "" {
		providerCfg.BaseURL = req.BaseURL
	}

	cfg.AI.Providers[req.Provider] = providerCfg

	if req.SetDefault {
		cfg.AI.DefaultProvider = req.Provider
	}

	if err := ds.saveConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ds *DashboardServer) deleteProvider(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Path[len("/api/ai/providers/"):]
	if providerName == "" {
		http.Error(w, "Provider name required", http.StatusBadRequest)
		return
	}

	cfg, err := ds.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	delete(cfg.AI.Providers, providerName)

	// If this was the default provider, switch to local
	if cfg.AI.DefaultProvider == providerName {
		cfg.AI.DefaultProvider = "local"
	}

	if err := ds.saveConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ds *DashboardServer) handleAITest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		Model    string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Create temporary provider for testing
	var provider ai.Provider
	switch req.Provider {
	case "openai":
		provider = ai.NewOpenAIProvider(req.APIKey)
	case "claude":
		provider = ai.NewClaudeProvider(req.APIKey)
	case "gemini":
		provider = ai.NewGeminiProvider(req.APIKey)
	case "github-copilot":
		provider = ai.NewGitHubCopilotProvider(req.APIKey)
	case "local":
		provider = ai.NewLocalProvider()
	default:
		http.Error(w, "Unsupported provider", http.StatusBadRequest)
		return
	}

	// Test with a simple prompt
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	options := ai.CompletionOptions{
		MaxTokens:   50,
		Temperature: 0.1,
		Purpose:     "test",
	}
	if req.Model != "" {
		options.Model = req.Model
	}

	_, err := provider.Complete(ctx, "Test connection. Please respond with 'OK'.", options)
	if err != nil {
		http.Error(w, fmt.Sprintf("Connection test failed: %v", err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Connection successful"))
}

func (ds *DashboardServer) loadConfig() (*config.Config, error) {
	configPath := ds.getConfigPath()
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err == nil {
			return &cfg, nil
		}
	}

	// Return default config
	return config.GetDefault(""), nil
}

func (ds *DashboardServer) saveConfig(cfg *config.Config) error {
	configPath := ds.getConfigPath()
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600) // Secure permissions for API keys
}

func (ds *DashboardServer) getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".yumem.yaml"
	}
	return filepath.Join(home, ".yumem.yaml")
}

func (ds *DashboardServer) getModelContextSize(providerType, modelID string) string {
	switch providerType {
	case "gemini":
		switch modelID {
		case "gemini-2.0-flash", "gemini-2.5-flash-preview":
			return "1M tokens"
		case "gemini-2.5-pro-preview":
			return "1M tokens"
		default:
			return "1M tokens"
		}
	case "openai":
		switch modelID {
		case "gpt-4o", "gpt-4o-mini":
			return "128K tokens"
		case "gpt-4-turbo-preview":
			return "128K tokens"
		default:
			return "128K tokens"
		}
	case "claude":
		return "200K tokens"
	case "github-copilot":
		return "128K tokens"
	default:
		return "8K tokens"
	}
}

// === AI Usage API ===

func (ds *DashboardServer) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if ds.aiManager == nil || ds.aiManager.Usage == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_calls":  0,
			"total_tokens": 0,
			"total_cost":   0,
			"by_provider":  map[string]interface{}{},
			"by_purpose":   map[string]interface{}{},
			"recent":       []interface{}{},
		})
		return
	}

	summary := ds.aiManager.Usage.GetSummary(20)
	json.NewEncoder(w).Encode(summary)
}

func (ds *DashboardServer) handleAIUsageReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ds.aiManager != nil && ds.aiManager.Usage != nil {
		ds.aiManager.Usage.Reset()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GitHub OAuth state storage (in production, use proper session storage)
var githubAuthStates = make(map[string]bool)

func (ds *DashboardServer) handleGitHubAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate state for CSRF protection
	state := "github-auth-" + fmt.Sprintf("%d", time.Now().UnixNano())
	githubAuthStates[state] = false

	// GitHub OAuth URL (in production, use proper GitHub App)
	authURL := fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=your_client_id&scope=copilot&state=%s", state)
	
	response := map[string]string{
		"authUrl": authURL,
		"state":   state,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ds *DashboardServer) handleGitHubAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// For demo purposes, simulate authentication
	// In production, check actual OAuth token status
	response := map[string]interface{}{
		"authenticated": false,
		"message":      "GitHub OAuth integration requires proper GitHub App setup",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ds *DashboardServer) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	
	if state == "" || code == "" {
		http.Error(w, "Missing state or code", http.StatusBadRequest)
		return
	}

	// Validate state
	if _, exists := githubAuthStates[state]; !exists {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Mark as authenticated (in production, exchange code for token)
	githubAuthStates[state] = true
	
	// Close window script
	html := `
	<html>
	<body>
		<script>
			window.close();
		</script>
		<p>Authentication complete. You can close this window.</p>
	</body>
	</html>
	`
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// === Memory Tools page and API ===

func (ds *DashboardServer) handleToolsPage(w http.ResponseWriter, r *http.Request) {
	ds.renderTemplate(w, "tools.html", map[string]interface{}{
		"Title": "Memory Tools",
		"Page":  "tools",
	})
}

func (ds *DashboardServer) handleAPICoreMemory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	coreMemory, err := ds.retrievalEngine.GetCoreMemory()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"content": coreMemory})
}

func (ds *DashboardServer) handleAPIRecallMemory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query().Get("query")
	if query == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "query parameter is required"})
		return
	}

	maxTopics := 5
	if mt := r.URL.Query().Get("max_topics"); mt != "" {
		if n, err := strconv.Atoi(mt); err == nil && n > 0 {
			maxTopics = n
		}
	}

	result, err := ds.retrievalEngine.RecallMemory(query, maxTopics)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (ds *DashboardServer) handleAPIStoreMemory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]string{"error": "POST required"})
		return
	}

	var req struct {
		Content string `json:"content"`
		Source  string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	if req.Content == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "content is required"})
		return
	}
	if req.Source == "" {
		req.Source = "web_dashboard"
	}

	// Store as standalone note
	title := fmt.Sprintf("note_%s", time.Now().Format("20060102_150405"))
	l2Tags := []string{"note", req.Source}
	entry, err := ds.l2Manager.AddEntry(title, req.Content, "note", req.Source, l2Tags)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Set metadata
	ds.l2Manager.UpdateMetadata(entry.ID, map[string]string{
		"content_type": "note",
		"source":       req.Source,
		"created_at":   time.Now().Format(time.RFC3339),
	})

	response := map[string]interface{}{
		"status":   "stored",
		"l2_id":    entry.ID,
		"analyzed": false,
	}

	// Run analysis if AI available
	if ds.aiManager != nil {
		bi := importers.NewBaseImporter(ds.l0Manager, ds.l1Manager, ds.l2Manager, ds.promptManager, ds.aiManager)
		if _, err := bi.AnalyzeAndApply(entry.ID, title, req.Content, req.Source, time.Time{}, nil); err == nil {
			response["analyzed"] = true
		}
	}

	json.NewEncoder(w).Encode(response)
}

// === Logs page and API ===

func (ds *DashboardServer) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	ds.renderTemplate(w, "logs.html", map[string]interface{}{
		"Title": "Logs",
		"Page":  "logs",
	})
}

func (ds *DashboardServer) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	logger := logging.Get()

	// Parse query params
	var levelFilter *logging.Level
	if lvl := r.URL.Query().Get("level"); lvl != "" {
		if parsed, ok := logging.ParseLevel(lvl); ok {
			levelFilter = &parsed
		}
	}
	component := r.URL.Query().Get("component")
	keyword := r.URL.Query().Get("q")
	sinceID := 0
	if s := r.URL.Query().Get("since_id"); s != "" {
		fmt.Sscanf(s, "%d", &sinceID)
	}
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	entries, latestID := logger.Query(levelFilter, component, keyword, sinceID, limit)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries":   entries,
		"latest_id": latestID,
	})
}