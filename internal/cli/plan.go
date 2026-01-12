package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/paths"
)

// planCmd is now for plan storage commands (write/read/list).
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Manage stored plans",
	Long: `Commands for managing stored plans.

Plans are markdown files stored in ~/.fab/plans/ (or $FAB_DIR/plans/).
They are created by planning agents using 'fab plan write'.

Examples:
  fab plan write              # Write plan from stdin (uses FAB_AGENT_ID)
  fab plan read abc123        # Read a stored plan
  fab plan list               # List all stored plans
`,
}

var planWriteCmd = &cobra.Command{
	Use:   "write",
	Short: "Write a plan from stdin",
	Long: `Write a plan from stdin using FAB_AGENT_ID as the plan ID.

The plan is read from stdin and saved to ~/.fab/plans/<id>.md.
The agent ID's "plan:" prefix is stripped if present.

Examples:
  echo "# My Plan" | fab plan write
  cat plan.md | fab plan write
`,
	RunE: runPlanWrite,
}

func runPlanWrite(cmd *cobra.Command, args []string) error {
	agentID := os.Getenv("FAB_AGENT_ID")
	if agentID == "" {
		return fmt.Errorf("FAB_AGENT_ID environment variable not set")
	}

	// Strip "plan:" prefix if present
	planID := strings.TrimPrefix(agentID, "plan:")

	// Read plan content from stdin
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Get plans directory and ensure it exists
	plansDir, err := paths.PlansDir()
	if err != nil {
		return fmt.Errorf("get plans directory: %w", err)
	}
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		return fmt.Errorf("create plans directory: %w", err)
	}

	// Write plan file
	planPath, err := paths.PlanPath(planID)
	if err != nil {
		return fmt.Errorf("get plan path: %w", err)
	}
	if err := os.WriteFile(planPath, content, 0644); err != nil {
		return fmt.Errorf("write plan file: %w", err)
	}

	fmt.Println(planID)
	return nil
}

var planReadCmd = &cobra.Command{
	Use:   "read <id>",
	Short: "Read a stored plan",
	Long: `Read a stored plan by ID and print its contents to stdout.

Examples:
  fab plan read abc123
`,
	Args: cobra.ExactArgs(1),
	RunE: runPlanRead,
}

func runPlanRead(cmd *cobra.Command, args []string) error {
	planID := args[0]

	planPath, err := paths.PlanPath(planID)
	if err != nil {
		return fmt.Errorf("get plan path: %w", err)
	}

	content, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plan not found: %s", planID)
		}
		return fmt.Errorf("read plan: %w", err)
	}

	fmt.Print(string(content))
	return nil
}

var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored plans",
	Long: `List all stored plans with their IDs and timestamps.

Plans are stored in ~/.fab/plans/ (or $FAB_DIR/plans/).

Examples:
  fab plan list
`,
	RunE: runPlanList,
}

func runPlanList(cmd *cobra.Command, args []string) error {
	plansDir, err := paths.PlansDir()
	if err != nil {
		return fmt.Errorf("get plans directory: %w", err)
	}

	entries, err := os.ReadDir(plansDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No stored plans")
			return nil
		}
		return fmt.Errorf("read plans directory: %w", err)
	}

	// Filter for .md files and collect info
	type planInfo struct {
		id      string
		modTime time.Time
	}
	var plans []planInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		planID := strings.TrimSuffix(name, ".md")
		plans = append(plans, planInfo{
			id:      planID,
			modTime: info.ModTime(),
		})
	}

	if len(plans) == 0 {
		fmt.Println("No stored plans")
		return nil
	}

	// Sort by modification time (newest first)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].modTime.After(plans[j].modTime)
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tMODIFIED")

	for _, p := range plans {
		_, _ = fmt.Fprintf(w, "%s\t%s\n", p.id, p.modTime.Format("2006-01-02 15:04"))
	}

	_ = w.Flush()
	return nil
}

func init() {
	planCmd.AddCommand(planWriteCmd)
	planCmd.AddCommand(planReadCmd)
	planCmd.AddCommand(planListCmd)

	rootCmd.AddCommand(planCmd)
}
