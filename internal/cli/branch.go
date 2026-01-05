package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Manage git branches",
	Long:  "Commands for managing agent branches created by fab.",
}

var branchCleanupDryRun bool
var branchCleanupLocal bool

var branchCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up merged fab/* branches",
	Long: `Delete fab/* branches that have been merged to main.

By default, only deletes remote branches. Use --local to also delete local refs.
Use --dry-run to see what would be deleted without making changes.`,
	Args: cobra.NoArgs,
	RunE: runBranchCleanup,
}

func runBranchCleanup(cmd *cobra.Command, args []string) error {
	// Get the current directory and ensure it's a git repo
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Fetch latest from origin to ensure we have current branch state
	if !branchCleanupDryRun {
		fetchCmd := exec.Command("git", "fetch", "--prune", "origin")
		fetchCmd.Dir = cwd
		if output, err := fetchCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fetch origin: %w\n%s", err, output)
		}
	}

	// Get merged remote branches matching fab/*
	remoteBranches, err := getMergedFabBranches(cwd, true)
	if err != nil {
		return fmt.Errorf("list merged remote branches: %w", err)
	}

	// Get merged local branches matching fab/*
	var localBranches []string
	if branchCleanupLocal {
		localBranches, err = getMergedFabBranches(cwd, false)
		if err != nil {
			return fmt.Errorf("list merged local branches: %w", err)
		}
	}

	if len(remoteBranches) == 0 && len(localBranches) == 0 {
		fmt.Println("🚌 No merged fab/* branches found")
		return nil
	}

	// Show what we found
	if branchCleanupDryRun {
		fmt.Println("🚌 Dry run - would delete the following branches:")
	} else {
		fmt.Println("🚌 Cleaning up merged fab/* branches:")
	}

	// Delete remote branches
	var deletedRemote, deletedLocal int
	for _, branch := range remoteBranches {
		// Branch comes as "origin/fab/xxx", extract just "fab/xxx"
		branchName := strings.TrimPrefix(branch, "origin/")
		if branchCleanupDryRun {
			fmt.Printf("   [remote] %s\n", branchName)
		} else {
			deleteCmd := exec.Command("git", "push", "origin", "--delete", branchName)
			deleteCmd.Dir = cwd
			if output, err := deleteCmd.CombinedOutput(); err != nil {
				fmt.Printf("   [remote] %s: failed (%v)\n", branchName, strings.TrimSpace(string(output)))
			} else {
				fmt.Printf("   [remote] %s: deleted\n", branchName)
				deletedRemote++
			}
		}
	}

	// Delete local branches
	for _, branch := range localBranches {
		if branchCleanupDryRun {
			fmt.Printf("   [local]  %s\n", branch)
		} else {
			deleteCmd := exec.Command("git", "branch", "-d", branch)
			deleteCmd.Dir = cwd
			if output, err := deleteCmd.CombinedOutput(); err != nil {
				fmt.Printf("   [local]  %s: failed (%v)\n", branch, strings.TrimSpace(string(output)))
			} else {
				fmt.Printf("   [local]  %s: deleted\n", branch)
				deletedLocal++
			}
		}
	}

	// Summary
	if !branchCleanupDryRun {
		if deletedRemote > 0 || deletedLocal > 0 {
			fmt.Printf("🚌 Deleted %d remote and %d local branch(es)\n", deletedRemote, deletedLocal)
		}
	} else {
		fmt.Printf("   Run without --dry-run to delete %d remote", len(remoteBranches))
		if branchCleanupLocal {
			fmt.Printf(" and %d local", len(localBranches))
		}
		fmt.Println(" branch(es)")
	}

	return nil
}

// getMergedFabBranches returns fab/* branches that have been merged to main.
// If remote is true, returns remote branches (origin/fab/*), otherwise local (fab/*).
func getMergedFabBranches(repoDir string, remote bool) ([]string, error) {
	// Use git branch --merged to find merged branches
	args := []string{"branch", "--merged", "origin/main"}
	if remote {
		args = append(args, "-r")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and HEAD pointer
		if line == "" || strings.Contains(line, "->") {
			continue
		}

		// Filter for fab/* branches
		if remote {
			// Remote branches are like "origin/fab/xxx"
			if strings.HasPrefix(line, "origin/fab/") {
				branches = append(branches, line)
			}
		} else {
			// Local branches are like "fab/xxx"
			if strings.HasPrefix(line, "fab/") {
				branches = append(branches, line)
			}
		}
	}

	return branches, nil
}

func init() {
	branchCleanupCmd.Flags().BoolVar(&branchCleanupDryRun, "dry-run", false, "Show what would be deleted without making changes")
	branchCleanupCmd.Flags().BoolVar(&branchCleanupLocal, "local", false, "Also delete local branch refs")

	branchCmd.AddCommand(branchCleanupCmd)
	rootCmd.AddCommand(branchCmd)
}
