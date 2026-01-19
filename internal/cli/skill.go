package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/config"
	"github.com/spf13/cobra"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage orch skill packages for LLM agents",
		Long: `Manage skill packages that teach LLM agents (OpenCode, Claude, Codex) how to use orch.

Use 'orch skill export' to export a skill package to a directory.`,
	}

	cmd.AddCommand(newSkillExportCmd())

	return cmd
}

func newSkillExportCmd() *cobra.Command {
	var stdout bool

	cmd := &cobra.Command{
		Use:   "export <dir>",
		Short: "Export orch skill package for LLM agents",
		Long: `Export a skill package that teaches LLM agents how to use orch.

The exported skill directory can be installed for:
  - OpenCode: ~/.config/opencode/skills/orch/ or .opencode/skills/orch/
  - Claude Code: ~/.claude/skills/orch/ or .claude/skills/orch/
  - Codex: ~/.codex/skills/orch/ or .codex/skills/orch/

Examples:
  # Export to OpenCode global skills
  orch skill export ~/.config/opencode/skills/orch/

  # Export to Claude Code global skills
  orch skill export ~/.claude/skills/orch/

  # Export to project-local skills
  orch skill export .opencode/skills/orch/

  # Export concatenated content to stdout for piping
  orch skill export --stdout`,
		Args: func(cmd *cobra.Command, args []string) error {
			stdout, _ := cmd.Flags().GetBool("stdout")
			if stdout {
				return nil
			}
			if len(args) < 1 {
				return fmt.Errorf("requires directory argument (or use --stdout)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if stdout {
				return exportToStdout()
			}
			return exportToDir(args[0])
		},
	}

	cmd.Flags().BoolVar(&stdout, "stdout", false, "Export concatenated content to stdout for piping")

	return cmd
}

func exportToDir(dir string) error {
	// Expand ~ in path
	if strings.HasPrefix(dir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dir = filepath.Join(home, dir[1:])
	}

	// Make path absolute
	if !filepath.IsAbs(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		dir = filepath.Join(cwd, dir)
	}

	// Create directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Get dynamic config for config.md
	configContent := generateConfigContent()

	// Write all skill files
	files := map[string]string{
		"SKILL.md":           skillMainContent,
		"commands.md":        skillCommandsContent,
		"workflows.md":       skillWorkflowsContent,
		"troubleshooting.md": skillTroubleshootingContent,
		"config.md":          configContent,
	}

	for filename, content := range files {
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
		fmt.Printf("Created %s\n", path)
	}

	fmt.Printf("\nSkill package exported to: %s\n", dir)
	fmt.Println("\nInstallation paths for each agent:")
	fmt.Println("  OpenCode:    ~/.config/opencode/skills/orch/ or .opencode/skills/orch/")
	fmt.Println("  Claude Code: ~/.claude/skills/orch/ or .claude/skills/orch/")
	fmt.Println("  Codex:       ~/.codex/skills/orch/ or .codex/skills/orch/")

	return nil
}

func exportToStdout() error {
	configContent := generateConfigContent()

	// Concatenate all content with separators
	fmt.Print(skillMainContent)
	fmt.Print("\n---\n\n")
	fmt.Print(skillCommandsContent)
	fmt.Print("\n---\n\n")
	fmt.Print(skillWorkflowsContent)
	fmt.Print("\n---\n\n")
	fmt.Print(skillTroubleshootingContent)
	fmt.Print("\n---\n\n")
	fmt.Print(configContent)

	return nil
}

func generateConfigContent() string {
	var sb strings.Builder

	sb.WriteString(skillConfigHeader)

	// Try to load current repo's config
	cfg, err := config.Load()
	if err == nil {
		sb.WriteString("\n## Current Repository Configuration\n\n")
		sb.WriteString("The following shows the detected configuration for this repository:\n\n")
		sb.WriteString("```yaml\n")

		if cfg.Agent != "" {
			sb.WriteString(fmt.Sprintf("agent: %s\n", cfg.Agent))
		}
		if cfg.Model != "" {
			sb.WriteString(fmt.Sprintf("model: %s\n", cfg.Model))
		}
		if cfg.ModelVariant != "" {
			sb.WriteString(fmt.Sprintf("model_variant: %s\n", cfg.ModelVariant))
		}
		if cfg.WorktreeDir != "" {
			sb.WriteString(fmt.Sprintf("worktree_dir: %s\n", cfg.WorktreeDir))
		}
		if cfg.BaseBranch != "" {
			sb.WriteString(fmt.Sprintf("base_branch: %s\n", cfg.BaseBranch))
		}
		if cfg.PRTargetBranch != "" {
			sb.WriteString(fmt.Sprintf("pr_target_branch: %s\n", cfg.PRTargetBranch))
		}
		if cfg.DefaultPreset != "" {
			sb.WriteString(fmt.Sprintf("default_preset: %s\n", cfg.DefaultPreset))
		}
		if cfg.Multiplexer != "" {
			sb.WriteString(fmt.Sprintf("multiplexer: %s\n", cfg.Multiplexer))
		}
		if cfg.NoPR {
			sb.WriteString("no_pr: true\n")
		}

		// Show presets if configured
		presets := cfg.GetAllPresets()
		if len(presets) > 0 {
			sb.WriteString("\npresets:\n")
			for _, p := range presets {
				sb.WriteString(fmt.Sprintf("  - name: %s\n", p.Name))
				if p.Backend != "" && p.Backend != "opencode" {
					sb.WriteString(fmt.Sprintf("    backend: %s\n", p.Backend))
				}
				if p.Model != "" {
					sb.WriteString(fmt.Sprintf("    model: %s\n", p.Model))
				}
				if p.Variant != "" {
					sb.WriteString(fmt.Sprintf("    variant: %s\n", p.Variant))
				}
				if p.Profile != "" {
					sb.WriteString(fmt.Sprintf("    profile: %s\n", p.Profile))
				}
			}
		}

		// Show issues config
		if cfg.Issues.Backend != "" || cfg.Issues.Path != "" {
			sb.WriteString("\nissues:\n")
			if cfg.Issues.Backend != "" {
				sb.WriteString(fmt.Sprintf("  backend: %s\n", cfg.Issues.Backend))
			}
			if cfg.Issues.Path != "" {
				sb.WriteString(fmt.Sprintf("  path: %s\n", cfg.Issues.Path))
			}
		}

		sb.WriteString("```\n")
	}

	sb.WriteString(skillConfigFooter)

	return sb.String()
}
