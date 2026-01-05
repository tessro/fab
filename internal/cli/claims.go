package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var claimsProject string

var claimsCmd = &cobra.Command{
	Use:   "claims",
	Short: "List active ticket claims",
	Long:  "Show which tickets are claimed by which agents to prevent duplicate work.",
	RunE:  runClaims,
}

func runClaims(cmd *cobra.Command, args []string) error {
	client := MustConnect()
	defer client.Close()

	resp, err := client.ClaimList(claimsProject)
	if err != nil {
		return fmt.Errorf("list claims: %w", err)
	}

	if len(resp.Claims) == 0 {
		if claimsProject != "" {
			fmt.Printf("No active claims for project %q\n", claimsProject)
		} else {
			fmt.Println("No active claims")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TICKET\tAGENT\tPROJECT")

	for _, c := range resp.Claims {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", c.TicketID, c.AgentID, c.Project)
	}

	_ = w.Flush()
	return nil
}

func init() {
	claimsCmd.Flags().StringVarP(&claimsProject, "project", "p", "", "Filter by project name")
	rootCmd.AddCommand(claimsCmd)
}
