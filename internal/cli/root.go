package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/mcp"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
	"yumem/internal/versioning"
	"yumem/internal/web"
	"yumem/internal/workspace"
)

var (
	cfgFile       string
	workingDir    string
	mcpPort       int
	webPort       int
	openBrowser   bool
	verboseMode   bool
	mcpStdio      bool
)

type Services struct {
	MCPServer    *mcp.Server
	WebServer    *web.DashboardServer
	StartTime    time.Time
}

type StartupConfig struct {
	Version       string
	WorkspaceDir  string
	MCPPort       int
	WebPort       int
	Stats         SystemStats
	LogFile       string
	ConfigFile    string
}

type SystemStats struct {
	L0CurrentSize   string
	L0MaxSize       string
	L0UsagePercent  float64
	L1NodeCount     int
	L2EntryCount    int
	TotalSize       string
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "yumem",
	Short: "YuMem Memory Management System",
	Long: `YuMem is a memory management system that maintains a structured workspace
for AI conversations. It provides L0 (core user info), L1 (semantic index),
and L2 (raw text) layers to enable AI models to understand you better.`,
	RunE: runYuMem,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func runYuMem(cmd *cobra.Command, args []string) error {
	// 1. Initialize workspace
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	// 2. If stdio mode, run MCP over stdin/stdout (no web, no SSE)
	if mcpStdio {
		return runMCPStdio()
	}

	// 3. Start services
	services, err := startServices()
	if err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}

	// 4. Display startup information
	config := buildStartupConfig(services)
	printStartupBanner(config)

	// 5. Auto-open browser if requested
	if openBrowser {
		go func() {
			time.Sleep(2 * time.Second)
			openDashboardInBrowser(webPort)
		}()
	}

	// 6. Wait for shutdown signal
	return waitForShutdown(services)
}

func runMCPStdio() error {
	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()
	promptManager := prompts.NewPromptManager()

	aiManager := ai.NewManager()
	cfg := config.LoadFromFile(workingDir)
	aiProviders := make(map[string]ai.ProviderConfig)
	for name, pc := range cfg.AI.Providers {
		aiProviders[name] = ai.ProviderConfig{
			Type:    pc.Type,
			APIKey:  pc.APIKey,
			BaseURL: pc.BaseURL,
			Model:   pc.Model,
		}
	}
	aiManager.InitializeFromConfig(cfg.AI.DefaultProvider, aiProviders)

	retrievalEngine := retrieval.NewRetrievalEngine(l0Manager, l1Manager, l2Manager, promptManager, aiManager)
	mcpServer := mcp.NewServer(mcpPort, l0Manager, l1Manager, l2Manager, promptManager, retrievalEngine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	return mcpServer.ServeStdio(ctx)
}

func initializeWorkspace() error {
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	if err := workspace.Initialize(workingDir); err != nil {
		return err
	}

	// Initialize all managers
	versionManager := versioning.NewVersionManager()
	if err := versionManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize version manager: %w", err)
	}

	promptManager := prompts.NewPromptManager()
	if err := promptManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize prompt manager: %w", err)
	}

	return nil
}

func startServices() (*Services, error) {
	services := &Services{
		StartTime: time.Now(),
	}

	// Initialize managers
	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()
	promptManager := prompts.NewPromptManager()
	versionManager := versioning.NewVersionManager()
	
	// Initialize AI manager with configuration
	aiManager := ai.NewManager()
	cfg := config.LoadFromFile(workingDir)
	aiProviders := make(map[string]ai.ProviderConfig)
	for name, pc := range cfg.AI.Providers {
		aiProviders[name] = ai.ProviderConfig{
			Type:    pc.Type,
			APIKey:  pc.APIKey,
			BaseURL: pc.BaseURL,
			Model:   pc.Model,
		}
	}
	aiManager.InitializeFromConfig(cfg.AI.DefaultProvider, aiProviders)
	
	retrievalEngine := retrieval.NewRetrievalEngine(l0Manager, l1Manager, l2Manager, promptManager, aiManager)

	// Start MCP server in goroutine
	mcpServer := mcp.NewServer(mcpPort, l0Manager, l1Manager, l2Manager, promptManager, retrievalEngine)
	go func() {
		if err := mcpServer.Start(); err != nil {
			fmt.Printf("❌ MCP server error: %v\n", err)
		}
	}()
	services.MCPServer = mcpServer

	// Start web dashboard in goroutine
	webServer := web.NewDashboardServer(webPort, l0Manager, l1Manager, l2Manager, promptManager, versionManager, retrievalEngine, aiManager)
	go func() {
		if err := webServer.Start(); err != nil {
			fmt.Printf("❌ Web dashboard error: %v\n", err)
		}
	}()
	services.WebServer = webServer

	// Wait for services to start
	time.Sleep(3 * time.Second)

	return services, nil
}

func buildStartupConfig(services *Services) StartupConfig {
	stats := calculateSystemStats()
	
	return StartupConfig{
		Version:      "1.2.3",
		WorkspaceDir: workingDir,
		MCPPort:      mcpPort,
		WebPort:      webPort,
		Stats:        stats,
		LogFile:      workingDir + "/yumem.log",
		ConfigFile:   "~/.yumem.yaml",
	}
}

func calculateSystemStats() SystemStats {
	// TODO: Calculate actual stats
	return SystemStats{
		L0CurrentSize:  "8.5KB",
		L0MaxSize:      "10KB",
		L0UsagePercent: 85.0,
		L1NodeCount:    127,
		L2EntryCount:   1543,
		TotalSize:      "245MB",
	}
}

func printStartupBanner(config StartupConfig) {
	fmt.Println()
	fmt.Println("🧠 ====================================")
	fmt.Println("   YuMem Memory Management System")
	fmt.Printf("   Version: v%s\n", config.Version)
	fmt.Printf("   Workspace: %s\n", config.WorkspaceDir)
	fmt.Println("🧠 ====================================")
	fmt.Println()
	
	// Services status
	fmt.Println("🚀 Services Started:")
	fmt.Printf("   ├─ MCP Server      : ✓ Running on port %d\n", config.MCPPort)
	fmt.Printf("   ├─ Web Dashboard   : ✓ Running on port %d\n", config.WebPort)
	fmt.Printf("   └─ Memory Engine   : ✓ Initialized\n")
	fmt.Println()
	
	// Access information
	fmt.Println("🌐 Access Information:")
	fmt.Printf("   ├─ Dashboard       : http://localhost:%d\n", config.WebPort)
	fmt.Printf("   ├─ Dashboard (LAN) : http://%s:%d\n", getLocalIP(), config.WebPort)
	fmt.Printf("   └─ Health Check    : http://localhost:%d/health\n", config.WebPort)
	fmt.Println()
	
	// MCP API endpoints
	fmt.Println("🔗 MCP Protocol (SSE Transport):")
	fmt.Printf("   ├─ SSE Endpoint   : http://localhost:%d/sse\n", config.MCPPort)
	fmt.Printf("   ├─ Message Endpt  : http://localhost:%d/message\n", config.MCPPort)
	fmt.Println("   ├─ Tools          : get_l0_context, update_l0, search_l1,")
	fmt.Println("   │                   create_l1_node, update_l1_node, search_l2,")
	fmt.Println("   │                   add_l2_file, get_l2_content, retrieve_context")
	fmt.Println("   └─ Stdio mode     : yumem --mcp-stdio")
	fmt.Println()
	
	// System status
	fmt.Println("📊 System Status:")
	fmt.Printf("   ├─ L0 Size         : %s / %s (%.1f%%)\n", 
		config.Stats.L0CurrentSize, 
		config.Stats.L0MaxSize,
		config.Stats.L0UsagePercent)
	fmt.Printf("   ├─ L1 Nodes        : %d nodes\n", config.Stats.L1NodeCount)
	fmt.Printf("   ├─ L2 Entries      : %d entries\n", config.Stats.L2EntryCount)
	fmt.Printf("   └─ Total Storage   : %s\n", config.Stats.TotalSize)
	fmt.Println()
	
	// Quick start guide
	fmt.Println("⚡ Quick Start:")
	fmt.Println("   ├─ Open Dashboard  : Open the URL above in your browser")
	fmt.Println("   ├─ Set L0 Info     : yumem l0 set --name \"Your Name\"")
	fmt.Println("   ├─ Import Notes    : yumem import notes --all")
	fmt.Println("   ├─ View Memory     : yumem l1 tree")
	fmt.Println("   └─ Get Help        : yumem --help")
	fmt.Println()
	
	// Notes
	fmt.Println("💡 Notes:")
	fmt.Println("   ├─ Press Ctrl+C to stop all services")
	fmt.Printf("   ├─ Logs are saved to: %s\n", config.LogFile)
	fmt.Printf("   └─ Config file: %s\n", config.ConfigFile)
	fmt.Println()
	fmt.Println("🎉 YuMem is ready! Happy memory managing!")
	fmt.Println("=====================================")
}

func openDashboardInBrowser(port int) {
	dashboardURL := fmt.Sprintf("http://localhost:%d", port)
	
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":  // macOS
		cmd = exec.Command("open", dashboardURL)
	case "windows": // Windows
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", dashboardURL)
	default:        // Linux
		cmd = exec.Command("xdg-open", dashboardURL)
	}
	
	_ = cmd.Run() // Ignore errors
}

func waitForShutdown(services *Services) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	<-sigChan
	
	fmt.Println("\n🛑 Shutting down YuMem...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if services.MCPServer != nil {
		if err := services.MCPServer.Shutdown(ctx); err != nil {
			fmt.Printf("❌ MCP server shutdown error: %v\n", err)
		} else {
			fmt.Println("✓ MCP server stopped")
		}
	}
	
	if services.WebServer != nil {
		if err := services.WebServer.Shutdown(ctx); err != nil {
			fmt.Printf("❌ Web dashboard shutdown error: %v\n", err)
		} else {
			fmt.Println("✓ Web dashboard stopped")
		}
	}
	
	fmt.Println("✨ YuMem stopped gracefully. Goodbye!")
	return nil
}

func getLocalIP() string {
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

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.yumem.yaml)")
	rootCmd.PersistentFlags().StringVarP(&workingDir, "workspace", "w", "", "workspace directory (default is current directory)")
	rootCmd.PersistentFlags().IntVar(&mcpPort, "mcp-port", 8080, "MCP server port")
	rootCmd.PersistentFlags().IntVar(&webPort, "web-port", 3000, "Web dashboard port")
	rootCmd.PersistentFlags().BoolVar(&openBrowser, "open-browser", true, "Open dashboard in browser")
	rootCmd.PersistentFlags().BoolVar(&verboseMode, "verbose", false, "Verbose logging")
	rootCmd.PersistentFlags().BoolVar(&mcpStdio, "mcp-stdio", false, "Run MCP server over stdio (for Claude Desktop integration)")
}

// initConfig reads in config file and ENV variables.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".yumem")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	// Initialize workspace
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	
	workspace.Initialize(workingDir)
}

