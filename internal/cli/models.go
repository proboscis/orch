package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/spf13/cobra"
)

type modelsOptions struct {
	Agent string
	Port  int
}

func newModelsCmd() *cobra.Command {
	opts := &modelsOptions{}

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models and variants",
		Long: `List available models and their thinking variants for opencode.

This command queries a running opencode server for available models.
If no server is running, it will report that.

Example output:
  Provider: anthropic
    claude-opus-4-5
      variants: default, high, max
    claude-sonnet-4-5
      variants: default, high, max

Use with --json for machine-readable output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModels(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Agent, "agent", "a", "opencode", "Agent type (only opencode currently supported)")
	cmd.Flags().IntVar(&opts.Port, "port", 4096, "OpenCode server port")

	return cmd
}

// modelInfo represents a model with its variants for output
type modelInfo struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Name     string   `json:"name,omitempty"`
	Variants []string `json:"variants,omitempty"`
}

type modelsResult struct {
	OK       bool        `json:"ok"`
	Agent    string      `json:"agent"`
	Models   []modelInfo `json:"models,omitempty"`
	Error    string      `json:"error,omitempty"`
	Fallback bool        `json:"fallback,omitempty"` // True if showing hardcoded defaults
}

func runModels(opts *modelsOptions) error {
	if opts.Agent != "opencode" {
		return fmt.Errorf("models command currently only supports opencode agent")
	}

	result := &modelsResult{
		OK:    true,
		Agent: opts.Agent,
	}

	// Try to connect to a running opencode server
	client := agent.NewOpenCodeClient(opts.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check for running server in port range
	serverPort := findRunningOpenCodeServer(opts.Port, opts.Port+100)
	if serverPort == 0 {
		// No server running - show fallback defaults
		result.Models = getDefaultModels()
		result.Fallback = true
		return outputModelsResult(result)
	}

	// Use the found port
	client = agent.NewOpenCodeClient(serverPort)
	providers, err := client.GetProviders(ctx)
	if err != nil {
		// Server running but can't get providers - show fallback
		result.Models = getDefaultModels()
		result.Fallback = true
		return outputModelsResult(result)
	}

	// Convert providers to model info
	models := []modelInfo{}
	for _, provider := range providers.All {
		for _, m := range provider.Models {
			info := modelInfo{
				Provider: provider.ID,
				Model:    m.ID,
				Name:     m.Name,
				Variants: m.Variants,
			}
			models = append(models, info)
		}
	}

	// Sort by provider then model
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Model < models[j].Model
	})

	result.Models = models
	return outputModelsResult(result)
}

func outputModelsResult(result *modelsResult) error {
	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !result.OK {
		return fmt.Errorf("%s", result.Error)
	}

	if result.Fallback {
		fmt.Println("Note: No opencode server running. Showing default models.")
		fmt.Println("Start a server with 'opencode serve' for live model discovery.")
		fmt.Println()
	}

	// Group by provider for display
	providerModels := make(map[string][]modelInfo)
	for _, m := range result.Models {
		providerModels[m.Provider] = append(providerModels[m.Provider], m)
	}

	// Sort provider names
	providers := make([]string, 0, len(providerModels))
	for p := range providerModels {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	for _, provider := range providers {
		fmt.Printf("Provider: %s\n", provider)
		for _, m := range providerModels[provider] {
			// Display model ID
			fmt.Printf("  %s/%s\n", provider, m.Model)
			// Display variants if any
			if len(m.Variants) > 0 {
				fmt.Printf("    variants: %s\n", strings.Join(m.Variants, ", "))
			}
		}
		fmt.Println()
	}

	return nil
}

// getDefaultModels returns hardcoded default models when no server is available
func getDefaultModels() []modelInfo {
	return []modelInfo{
		{
			Provider: "anthropic",
			Model:    "claude-opus-4-5",
			Variants: []string{"default", "high", "max"},
		},
		{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-5",
			Variants: []string{"default", "high", "max"},
		},
		{
			Provider: "anthropic",
			Model:    "claude-haiku-4-5",
			Variants: []string{"default", "high", "max"},
		},
		{
			Provider: "openai",
			Model:    "gpt-4o",
			Variants: []string{},
		},
		{
			Provider: "openai",
			Model:    "o1-preview",
			Variants: []string{},
		},
		{
			Provider: "openai",
			Model:    "o1-mini",
			Variants: []string{},
		},
		{
			Provider: "google",
			Model:    "gemini-2.5-pro",
			Variants: []string{"low", "high", "max"},
		},
		{
			Provider: "google",
			Model:    "gemini-2.5-flash",
			Variants: []string{"low", "high", "max"},
		},
	}
}
