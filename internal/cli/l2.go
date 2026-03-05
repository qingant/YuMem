package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"yumem/internal/memory"
)

var l2Cmd = &cobra.Command{
	Use:   "l2",
	Short: "Manage L2 (raw text index)",
	Long:  `Manage L2 layer which contains raw text indexing system.`,
}

var l2AddCmd = &cobra.Command{
	Use:   "add [file-path]",
	Short: "Add a file to L2 index",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l2Manager := memory.NewL2Manager()
		
		filePath := args[0]
		tags, _ := cmd.Flags().GetStringSlice("tag")

		entry, err := l2Manager.AddFile(filePath, tags)
		if err != nil {
			return err
		}

		fmt.Printf("Added file to L2 index: %s (ID: %s)\n", entry.FilePath, entry.ID)
		return nil
	},
}

var l2SearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search L2 entries",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l2Manager := memory.NewL2Manager()
		
		var query string
		if len(args) > 0 {
			query = args[0]
		}
		tags, _ := cmd.Flags().GetStringSlice("tag")

		entries, err := l2Manager.SearchEntries(query, tags)
		if err != nil {
			return err
		}

		if len(entries) == 0 {
			fmt.Println("No entries found")
			return nil
		}

		for _, entry := range entries {
			fmt.Printf("ID: %s\n", entry.ID)
			fmt.Printf("File: %s\n", entry.FilePath)
			fmt.Printf("Size: %d bytes\n", entry.Size)
			fmt.Printf("Type: %s\n", entry.MimeType)
			fmt.Printf("Tags: %s\n", strings.Join(entry.Tags, ", "))
			fmt.Printf("Updated: %s\n", entry.UpdatedAt.Format("2006-01-02 15:04:05"))
			fmt.Println("---")
		}
		return nil
	},
}

var l2ShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show L2 entry content",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l2Manager := memory.NewL2Manager()
		
		id := args[0]
		content, err := l2Manager.GetContent(id)
		if err != nil {
			return err
		}

		fmt.Print(string(content))
		return nil
	},
}

var l2UpdateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update L2 entry metadata",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l2Manager := memory.NewL2Manager()
		
		id := args[0]
		tags, _ := cmd.Flags().GetStringSlice("tag")
		metadataStrings, _ := cmd.Flags().GetStringSlice("metadata")
		
		// Parse metadata
		var metadata map[string]string
		if len(metadataStrings) > 0 {
			metadata = make(map[string]string)
			for _, meta := range metadataStrings {
				parts := strings.SplitN(meta, "=", 2)
				if len(parts) == 2 {
					metadata[parts[0]] = parts[1]
				}
			}
		}

		err := l2Manager.UpdateFile(id, tags, metadata)
		if err != nil {
			return err
		}

		fmt.Printf("Updated L2 entry: %s\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(l2Cmd)
	l2Cmd.AddCommand(l2AddCmd)
	l2Cmd.AddCommand(l2SearchCmd)
	l2Cmd.AddCommand(l2ShowCmd)
	l2Cmd.AddCommand(l2UpdateCmd)

	l2AddCmd.Flags().StringSlice("tag", []string{}, "Tags for the file")
	l2SearchCmd.Flags().StringSlice("tag", []string{}, "Filter by tags")
	l2UpdateCmd.Flags().StringSlice("tag", []string{}, "New tags")
	l2UpdateCmd.Flags().StringSlice("metadata", []string{}, "Metadata (key=value format)")
}