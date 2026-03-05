package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"yumem/internal/workspace"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize YuMem workspace",
	Long:  `Initialize a YuMem workspace in the current directory or specified directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceDir := workingDir
		if workspaceDir == "" {
			var err error
			workspaceDir, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		fmt.Printf("Initializing YuMem workspace in: %s\n", workspaceDir)
		
		if err := workspace.Initialize(workspaceDir); err != nil {
			return fmt.Errorf("failed to initialize workspace: %w", err)
		}

		fmt.Println("✓ YuMem workspace initialized successfully")
		fmt.Println("✓ Directory structure created")
		fmt.Println("✓ Configuration files prepared")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. Set your L0 information: yumem l0 set --name \"Your Name\" --context \"Your context\"")
		fmt.Println("  2. Start the MCP server: yumem server")
		fmt.Println("  3. Add files to L2 index: yumem l2 add [file-path]")
		
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}