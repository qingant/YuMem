package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"yumem/internal/memory"
)

var l1Cmd = &cobra.Command{
	Use:   "l1",
	Short: "Manage L1 (semantic index tree)",
	Long:  `Manage L1 layer which contains semantic index tree with LLM-generated summaries.`,
}

var l1SearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search L1 nodes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l1Manager := memory.NewL1Manager()
		nodes, err := l1Manager.SearchNodes(args[0])
		if err != nil {
			return err
		}

		if len(nodes) == 0 {
			fmt.Println("No nodes found")
			return nil
		}

		for _, node := range nodes {
			fmt.Printf("ID: %s\n", node.ID)
			fmt.Printf("Path: %s\n", node.Path)
			fmt.Printf("Title: %s\n", node.Title)
			fmt.Printf("Summary: %s\n", node.Summary)
			fmt.Printf("Keywords: %s\n", strings.Join(node.Keywords, ", "))
			fmt.Printf("L2 Refs: %s\n", strings.Join(node.L2Refs, ", "))
			fmt.Println("---")
		}
		return nil
	},
}

var l1CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new L1 node",
	RunE: func(cmd *cobra.Command, args []string) error {
		l1Manager := memory.NewL1Manager()
		
		path, _ := cmd.Flags().GetString("path")
		title, _ := cmd.Flags().GetString("title")
		summary, _ := cmd.Flags().GetString("summary")
		keywords, _ := cmd.Flags().GetStringSlice("keyword")
		l2Refs, _ := cmd.Flags().GetStringSlice("l2-ref")

		if path == "" || title == "" {
			return fmt.Errorf("path and title are required")
		}

		node, err := l1Manager.CreateNode(path, title, summary, keywords, l2Refs)
		if err != nil {
			return err
		}

		fmt.Printf("Created L1 node: %s (ID: %s)\n", node.Title, node.ID)
		return nil
	},
}

var l1UpdateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an existing L1 node",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l1Manager := memory.NewL1Manager()
		
		id := args[0]
		summary, _ := cmd.Flags().GetString("summary")
		keywords, _ := cmd.Flags().GetStringSlice("keyword")

		err := l1Manager.UpdateNode(id, summary, keywords)
		if err != nil {
			return err
		}

		fmt.Printf("Updated L1 node: %s\n", id)
		return nil
	},
}

var l1TreeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Show L1 tree structure",
	RunE: func(cmd *cobra.Command, args []string) error {
		l1Manager := memory.NewL1Manager()
		nodes, err := l1Manager.GetTree()
		if err != nil {
			return err
		}

		if len(nodes) == 0 {
			fmt.Println("No nodes in tree")
			return nil
		}

		// Find root nodes (no parent)
		var rootNodes []*memory.L1Node
		for _, node := range nodes {
			if node.Parent == "" {
				rootNodes = append(rootNodes, node)
			}
		}

		// Print tree structure
		for _, root := range rootNodes {
			printNode(root, nodes, 0)
		}
		return nil
	},
}

func printNode(node *memory.L1Node, allNodes map[string]*memory.L1Node, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%s (%s)\n", indent, node.Title, node.Path)
	
	for _, childID := range node.Children {
		if child, exists := allNodes[childID]; exists {
			printNode(child, allNodes, depth+1)
		}
	}
}

func init() {
	rootCmd.AddCommand(l1Cmd)
	l1Cmd.AddCommand(l1SearchCmd)
	l1Cmd.AddCommand(l1CreateCmd)
	l1Cmd.AddCommand(l1UpdateCmd)
	l1Cmd.AddCommand(l1TreeCmd)

	l1CreateCmd.Flags().String("path", "", "Node path (required)")
	l1CreateCmd.Flags().String("title", "", "Node title (required)")
	l1CreateCmd.Flags().String("summary", "", "Node summary")
	l1CreateCmd.Flags().StringSlice("keyword", []string{}, "Keywords")
	l1CreateCmd.Flags().StringSlice("l2-ref", []string{}, "L2 references")

	l1UpdateCmd.Flags().String("summary", "", "New summary")
	l1UpdateCmd.Flags().StringSlice("keyword", []string{}, "New keywords")
}