package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/daemon"
)

type eventResult struct {
	event *daemon.StreamEvent
	err   error
}

var attachCmd = &cobra.Command{
	Use:   "attach [projects...]",
	Short: "Attach to agent streams and watch output",
	Long:  "Connect to the daemon and stream live output from running agents. Optionally filter by project names.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		// Attach to specified projects (or all if none specified)
		if err := client.Attach(args); err != nil {
			return fmt.Errorf("attach: %w", err)
		}

		fmt.Println("🚌 Attached to agent streams (Ctrl+C to detach)")

		// Set up signal handling
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)

		// Receive events in a goroutine
		eventCh := make(chan eventResult)
		go func() {
			for {
				event, err := client.RecvEvent()
				eventCh <- eventResult{event, err}
				if err != nil {
					return
				}
			}
		}()

		// Main loop: handle events and signals
		for {
			select {
			case <-sigCh:
				fmt.Println()
				if err := client.Detach(); err != nil {
					return fmt.Errorf("detach: %w", err)
				}
				fmt.Println("🚌 Detached")
				return nil

			case result := <-eventCh:
				if result.err != nil {
					if result.err == io.EOF {
						fmt.Println("🚌 Connection closed")
						return nil
					}
					return fmt.Errorf("receive event: %w", result.err)
				}
				displayEvent(result.event)
			}
		}
	},
}

func displayEvent(event *daemon.StreamEvent) {
	switch event.Type {
	case "output":
		fmt.Printf("[%s:%s] %s\n", event.Project, event.AgentID, event.Data)
	case "state":
		fmt.Printf("[%s:%s] State: %s\n", event.Project, event.AgentID, event.State)
	case "created":
		fmt.Printf("[%s] Agent created: %s\n", event.Project, event.AgentID)
	case "deleted":
		fmt.Printf("[%s] Agent deleted: %s\n", event.Project, event.AgentID)
	default:
		fmt.Printf("[%s:%s] %s: %s\n", event.Project, event.AgentID, event.Type, event.Data)
	}
}

func init() {
	rootCmd.AddCommand(attachCmd)
}
