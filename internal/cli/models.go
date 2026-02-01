package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

const defaultOpenCodePort = agent.OpenCodeServerPortStart

type modelsOptions struct {
	Port    int
	Timeout int
}

func newModelsCmd() *cobra.Command {
	opts := &modelsOptions{}

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models for opencode",
		Long: `List available models and variants for the opencode agent.

Requires a running opencode server. If no server is running, start one with:
  opencode serve --port 4096`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModels(opts)
		},
	}

	cmd.Flags().IntVar(&opts.Port, "port", defaultOpenCodePort, "OpenCode server port")
	cmd.Flags().IntVar(&opts.Timeout, "timeout", 5, "Timeout in seconds")

	return cmd
}

type modelsOutput struct {
	Providers []providerOutput `json:"providers"`
}

type providerOutput struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Models []modelOutput `json:"models"`
}

type modelOutput struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Variants []string `json:"variants,omitempty"`
}

func runModels(opts *modelsOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout)*time.Second)
	defer cancel()

	api := orchapi.NewDaemonClient("", "")
	result, err := api.QueryOpenCodeServer(ctx, opts.Port)
	if err != nil {
		return fmt.Errorf("failed to query opencode server: %w", err)
	}

	if !result.ServerRunning {
		return fmt.Errorf("opencode server not running on port %d\nStart with: opencode serve --port %d", opts.Port, opts.Port)
	}

	if globalOpts.JSON {
		output := modelsOutput{Providers: make([]providerOutput, 0, len(result.Providers))}
		for _, p := range result.Providers {
			po := providerOutput{
				ID:     p.ID,
				Name:   p.Name,
				Models: make([]modelOutput, 0, len(p.Models)),
			}
			for _, m := range p.Models {
				po.Models = append(po.Models, modelOutput{
					ID:       m.ID,
					Name:     m.Name,
					Variants: m.Variants,
				})
			}
			output.Providers = append(output.Providers, po)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	for _, p := range result.Providers {
		fmt.Printf("Provider: %s\n", p.ID)
		for _, m := range p.Models {
			fmt.Printf("  %s/%s\n", p.ID, m.ID)
			if len(m.Variants) > 0 {
				fmt.Printf("    variants: %s\n", strings.Join(m.Variants, ", "))
			}
		}
		fmt.Println()
	}

	return nil
}
