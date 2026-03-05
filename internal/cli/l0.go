package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"yumem/internal/memory"
)

var l0Cmd = &cobra.Command{
	Use:   "l0",
	Short: "Manage L0 (core user information)",
	Long:  `Manage L0 layer which contains core user information that's always included in conversations.`,
}

var l0ShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current L0 context",
	RunE: func(cmd *cobra.Command, args []string) error {
		l0Manager := memory.NewL0Manager()
		context, err := l0Manager.GetContext()
		if err != nil {
			return err
		}
		fmt.Print(context)
		return nil
	},
}

var l0SetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set L0 information",
	Long:  `Set user information in L0 layer. Use flags to specify what to update.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l0Manager := memory.NewL0Manager()
		
		userID, _ := cmd.Flags().GetString("user-id")
		name, _ := cmd.Flags().GetString("name")
		context, _ := cmd.Flags().GetString("context")
		prefStrings, _ := cmd.Flags().GetStringSlice("preference")
		
		// Parse preferences
		var preferences map[string]string
		if len(prefStrings) > 0 {
			preferences = make(map[string]string)
			for _, pref := range prefStrings {
				parts := strings.SplitN(pref, "=", 2)
				if len(parts) == 2 {
					preferences[parts[0]] = parts[1]
				}
			}
		}
		
		err := l0Manager.Update(userID, name, context, preferences)
		if err != nil {
			return err
		}
		
		fmt.Println("L0 information updated successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(l0Cmd)
	l0Cmd.AddCommand(l0ShowCmd)
	l0Cmd.AddCommand(l0SetCmd)
	
	l0SetCmd.Flags().String("user-id", "", "User ID")
	l0SetCmd.Flags().String("name", "", "User name")
	l0SetCmd.Flags().String("context", "", "User context")
	l0SetCmd.Flags().StringSlice("preference", []string{}, "User preferences (key=value format)")
}