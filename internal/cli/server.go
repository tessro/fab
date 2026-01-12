package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/plugin"
	"github.com/tessro/fab/internal/registry"
	"github.com/tessro/fab/internal/supervisor"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the fab daemon server",
	Long:  "Commands for managing the fab daemon server lifecycle.",
}

var serverStartForeground bool

var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the fab daemon server",
	Long:  "Start the fab daemon server. By default, daemonizes to the background.",
	RunE:  runServerStart,
}

var serverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the fab daemon server",
	Long:  "Stop the running fab daemon server. This will terminate all agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.Shutdown(); err != nil {
			return fmt.Errorf("shutdown daemon: %w", err)
		}

		fmt.Println("🚌 fab daemon stopped")
		return nil
	},
}

var serverRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the fab daemon server",
	Long:  "Stop the fab daemon server if running, then start it again.",
	RunE:  runServerRestart,
}

func runServerRestart(cmd *cobra.Command, args []string) error {
	pidPath := daemon.DefaultPIDPath()

	// Try to stop the daemon if it's running
	client, err := ConnectClient()
	if err == nil {
		defer client.Close()
		if err := client.Shutdown(); err != nil {
			return fmt.Errorf("shutdown daemon: %w", err)
		}

		fmt.Println("🚌 fab daemon stopped")

		// Wait for daemon to fully stop (up to 5 seconds)
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if !IsDaemonRunning() {
				break
			}
		}
	} else if !errors.Is(err, ErrDaemonNotRunning) {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	// Clean up any stale PID file
	daemon.CleanStalePID(pidPath)

	// Start the daemon
	return daemonize()
}

func runServerStart(cmd *cobra.Command, args []string) error {
	pidPath := daemon.DefaultPIDPath()

	// Check if already running
	if running, pid := daemon.IsDaemonRunning(pidPath); running {
		fmt.Printf("🚌 fab daemon is already running (pid %d)\n", pid)
		return nil
	}

	// Clean up stale PID file if present
	daemon.CleanStalePID(pidPath)

	// If not foreground mode, daemonize by re-executing with --foreground
	if !serverStartForeground {
		return daemonize()
	}

	// Foreground mode: run the daemon directly
	return runDaemon()
}

// daemonize re-executes the current process in background mode.
func daemonize() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}

	// Build command with --foreground flag
	args := []string{"server", "start", "--foreground"}

	// Pass --fab-dir if it was specified
	if fabDir := FabDir(); fabDir != "" {
		args = append([]string{"--fab-dir", fabDir}, args...)
	}

	cmd := exec.Command(exe, args...)

	// Detach from terminal
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("🚌 fab daemon started (pid %d)\n", cmd.Process.Pid)
	return nil
}

// runDaemon runs the daemon server in the foreground.
func runDaemon() error {
	// Load global config for log level
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logLevel := logging.ParseLevel(cfg.GetLogLevel())

	// Initialize logging
	logCleanup, err := logging.Setup("", logLevel)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	defer logCleanup()

	// Install Claude Code plugin (fresh install every startup)
	pluginDir := plugin.DefaultInstallDir()
	if err := plugin.Install(pluginDir); err != nil {
		slog.Warn("failed to install Claude Code plugin", "error", err)
		// Continue without plugin - not fatal
	} else {
		slog.Info("installed Claude Code plugin", "dir", pluginDir)
	}

	pidPath := daemon.DefaultPIDPath()

	// Write PID file
	if err := daemon.WritePID(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer func() { _ = daemon.RemovePID(pidPath) }()

	// Load registry
	reg, err := registry.New()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	// Create agent manager
	mgr := agent.NewManager()

	// Register all projects with manager
	for _, proj := range reg.List() {
		mgr.RegisterProject(proj)
	}

	// Create supervisor
	sup := supervisor.New(reg, mgr)

	// Create and start daemon server
	srv := daemon.NewServer("", sup)
	sup.SetServer(srv)

	if err := srv.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	defer func() { _ = srv.Stop() }()

	// Start orchestration for projects with autostart=true
	sup.StartAutostart()

	fmt.Println("🚌 fab daemon running...")

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal or supervisor shutdown
	select {
	case sig := <-sigCh:
		fmt.Printf("\n🚌 received %s, shutting down...\n", sig)
	case <-sup.ShutdownCh():
		fmt.Println("🚌 shutdown requested, stopping...")
	}

	// Stop all orchestrators and agents gracefully with timeout
	if sup.ShutdownWithTimeout(supervisor.ShutdownTimeout) {
		fmt.Println("🚌 fab daemon stopped")
	} else {
		fmt.Println("🚌 fab daemon stopped (some agents may not have stopped gracefully)")
	}
	return nil
}

func init() {
	serverStartCmd.Flags().BoolVarP(&serverStartForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	serverCmd.AddCommand(serverStartCmd)
	serverCmd.AddCommand(serverStopCmd)
	serverCmd.AddCommand(serverRestartCmd)
	rootCmd.AddCommand(serverCmd)
}
