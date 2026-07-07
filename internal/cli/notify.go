package cli

import (
	"context"
	"fmt"

	"github.com/proboscis/orch/internal/config"
	"github.com/proboscis/orch/internal/notify"
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
	ctx := context.Background()
	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	cfg, err := api.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.Slack.Enabled || (cfg.Slack.WebhookURL == "" && (cfg.Slack.BotToken == "" || cfg.Slack.Channel == "")) {
		return fmt.Errorf("Slack not configured. Add slack config to .orch/config.yaml")
	}

	slackCfg := &config.SlackConfig{
		Enabled:    cfg.Slack.Enabled,
		WebhookURL: cfg.Slack.WebhookURL,
		BotToken:   cfg.Slack.BotToken,
		Channel:    cfg.Slack.Channel,
		NotifyOn:   cfg.Slack.NotifyOn,
	}
	notifier := notify.NewSlackNotifier(slackCfg)

	channelName, err := notifier.SendTest(opts.Message)
	if err != nil {
		return fmt.Errorf("Slack API error: %w", err)
	}

	fmt.Printf("Slack notification sent successfully to %s\n", channelName)
	return nil
}
