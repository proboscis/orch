package cli

import (
	"fmt"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/notify"
	"github.com/spf13/cobra"
)

type notifyTestOptions struct {
	Message string
}

func newNotifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Notification management commands",
	}

	cmd.AddCommand(newNotifyTestCmd())

	return cmd
}

func newNotifyTestCmd() *cobra.Command {
	opts := &notifyTestOptions{}

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test notification to verify Slack configuration",
		Long: `Send a test notification to verify Slack is configured correctly.

Uses configuration from .orch/config.yaml or ~/.config/orch/config.yaml.

Examples:
  orch notify test
  orch notify test --message "Custom test message"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyTest(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Message, "message", "m", "", "Custom test message")

	return cmd
}

func runNotifyTest(opts *notifyTestOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.Slack.IsConfigured() {
		return fmt.Errorf("Slack not configured. Add slack config to .orch/config.yaml")
	}

	notifier := notify.NewSlackNotifier(&cfg.Slack)

	channelName, err := notifier.SendTest(opts.Message)
	if err != nil {
		return fmt.Errorf("Slack API error: %w", err)
	}

	fmt.Printf("Slack notification sent successfully to %s\n", channelName)
	return nil
}
