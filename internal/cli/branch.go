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
// This function handles both regular merges and rebased merges by using git cherry
// to detect branches where all commits have equivalent commits in main.
func getMergedFabBranches(repoDir string, remote bool) ([]string, error) {
	// List all branches matching fab/*
	args := []string{"branch"}
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
		var isFabBranch bool
		if remote {
			isFabBranch = strings.HasPrefix(line, "origin/fab/")
		} else {
			isFabBranch = strings.HasPrefix(line, "fab/")
		}
		if !isFabBranch {
			continue
		}

		// Check if branch is merged using git cherry
		// A branch is merged if all its commits have equivalents in main
		if isBranchMerged(repoDir, line) {
			branches = append(branches, line)
		}
	}

	return branches, nil
}

// isBranchMerged checks if a branch has been merged to origin/main.
// It uses git cherry to detect commits that have equivalent commits in main,
// which handles both regular merges and rebased merges.
func isBranchMerged(repoDir, branch string) bool {
	// Get the merge base between the branch and main
	mergeBaseCmd := exec.Command("git", "merge-base", branch, "origin/main")
	mergeBaseCmd.Dir = repoDir
	mergeBaseOutput, err := mergeBaseCmd.Output()
	if err != nil {
		return false
	}
	mergeBase := strings.TrimSpace(string(mergeBaseOutput))
	if mergeBase == "" {
		return false
	}

	// Use git cherry to find commits not yet in main
	// Commits with "+" prefix are NOT in main
	// Commits with "-" prefix have equivalent commits in main
	// If there are no "+" commits, the branch is merged
	cherryCmd := exec.Command("git", "cherry", "origin/main", branch, mergeBase)
	cherryCmd.Dir = repoDir
	cherryOutput, err := cherryCmd.Output()
	if err != nil {
		return false
	}

	// If output is empty, branch has no unique commits (already at merge base)
	output := strings.TrimSpace(string(cherryOutput))
	if output == "" {
		return true
	}

	// Check if any commits are not yet in main (have "+" prefix)
	cherryScanner := bufio.NewScanner(strings.NewReader(output))
	for cherryScanner.Scan() {
		line := cherryScanner.Text()
		if strings.HasPrefix(line, "+ ") {
			// Found a commit not in main
			return false
		}
	}

	// All commits have equivalents in main
	return true
}

func init() {
	branchCleanupCmd.Flags().BoolVar(&branchCleanupDryRun, "dry-run", false, "Show what would be deleted without making changes")
	branchCleanupCmd.Flags().BoolVar(&branchCleanupLocal, "local", false, "Also delete local branch refs")

	branchCmd.AddCommand(branchCleanupCmd)
	rootCmd.AddCommand(branchCmd)
}
