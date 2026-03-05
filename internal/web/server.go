package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"time"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
	"yumem/internal/versioning"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*
var templateFiles embed.FS

type DashboardServer struct {
	port            int
	server          *http.Server
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
	versionManager  *versioning.VersionManager
	retrievalEngine *retrieval.RetrievalEngine
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

func NewDashboardServer(port int, l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, versionManager *versioning.VersionManager, retrievalEngine *retrieval.RetrievalEngine) *DashboardServer {
	return &DashboardServer{
		port:            port,
		l0Manager:       l0Manager,
		l1Manager:       l1Manager,
		l2Manager:       l2Manager,
		promptManager:   promptManager,
		versionManager:  versionManager,
		retrievalEngine: retrievalEngine,
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

	// API endpoints
	mux.HandleFunc("/api/stats", ds.handleAPIStats)
	mux.HandleFunc("/api/prompts", ds.handleAPIPrompts)
	mux.HandleFunc("/api/prompts/test", ds.handleAPIPromptTest)
	mux.HandleFunc("/api/memory/l0", ds.handleAPIL0)
	mux.HandleFunc("/api/memory/l1", ds.handleAPIL1)
	mux.HandleFunc("/api/memory/l2", ds.handleAPIL2)
	mux.HandleFunc("/api/version", ds.handleAPIVersion)

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

	ds.renderTemplate(w, "dashboard.html", map[string]interface{}{
		"Title": "YuMem Dashboard",
		"Stats": stats,
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
		"Prompts":    prompts,
		"Categories": categories,
	})
}

func (ds *DashboardServer) handleMemory(w http.ResponseWriter, r *http.Request) {
	l0Data, _ := ds.l0Manager.Load()
	l1Tree, _ := ds.l1Manager.GetTree()

	ds.renderTemplate(w, "memory.html", map[string]interface{}{
		"Title":  "Memory Management",
		"L0Data": l0Data,
		"L1Tree": l1Tree,
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

func (ds *DashboardServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"uptime":    "1h 23m", // TODO: calculate actual uptime
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

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ds *DashboardServer) getSystemStats() (*SystemStats, error) {
	stats := &SystemStats{}

	// Memory stats
	_, err := ds.l0Manager.Load()
	if err == nil {
		stats.MemoryStats.L0CurrentKB = 8.5 // TODO: calculate actual size
		stats.MemoryStats.L0MaxKB = 10
		stats.MemoryStats.L0Usage = (stats.MemoryStats.L0CurrentKB / float64(stats.MemoryStats.L0MaxKB)) * 100
		stats.MemoryStats.L0Size = fmt.Sprintf("%.1fKB", stats.MemoryStats.L0CurrentKB)
	}

	l1Tree, err := ds.l1Manager.GetTree()
	if err == nil {
		stats.MemoryStats.L1NodeCount = len(l1Tree)
	}

	// TODO: Get actual L2 count
	stats.MemoryStats.L2EntryCount = 1247
	stats.MemoryStats.TotalSize = "245MB"

	// Usage stats
	stats.UsageStats.MCPRequests = 1523
	stats.UsageStats.RetrievalCalls = 89
	stats.UsageStats.StorageOps = 456
	stats.UsageStats.UpTime = "2h 15m"

	// Prompt stats
	prompts, err := ds.promptManager.ListPrompts("")
	if err == nil {
		stats.PromptStats.TotalTemplates = len(prompts)
		stats.PromptStats.RecentlyUpdated = 3
		stats.PromptStats.MostUsedTemplate = "L0 Context Formatting"
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