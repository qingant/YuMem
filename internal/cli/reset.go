package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"yumem/internal/workspace"
)

var resetConfirm bool

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset workspace by clearing all memory data",
	Long: `Reset the YuMem workspace by removing all L0, L1, and L2 data.
This will delete all memory layers and reinitialize the workspace structure.
Use --yes to skip the confirmation prompt.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wsDir := workingDir
		if wsDir == "" {
			var err error
			wsDir, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		yumemDir := filepath.Join(wsDir, "_yumem")
		if _, err := os.Stat(yumemDir); os.IsNotExist(err) {
			return fmt.Errorf("no YuMem workspace found at %s", wsDir)
		}

		if !resetConfirm {
			fmt.Printf("This will delete ALL memory data in: %s\n", yumemDir)
			fmt.Print("Are you sure? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		fmt.Println("Resetting workspace...")

		if err := os.RemoveAll(yumemDir); err != nil {
			return fmt.Errorf("failed to remove _yumem directory: %w", err)
		}

		if err := workspace.Initialize(wsDir); err != nil {
			return fmt.Errorf("failed to reinitialize workspace: %w", err)
		}

		fmt.Println("Workspace reset complete. All memory data has been cleared.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
	resetCmd.Flags().BoolVar(&resetConfirm, "yes", false, "Skip confirmation prompt")
}
