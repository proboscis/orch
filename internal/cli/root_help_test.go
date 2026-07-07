package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpRendersGroupedCommands(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
	})

	if err := rootCmd.Help(); err != nil {
		t.Fatalf("root help failed: %v", err)
	}

	help := out.String()
	for _, section := range []string{
		"\nCore Commands:\n",
		"\nSetup & Ops Commands:\n",
		"\nAdvanced Commands:\n",
	} {
		if !strings.Contains(help, section) {
			t.Fatalf("expected grouped help section %q in:\n%s", section, help)
		}
	}
	if strings.Contains(help, "\nAdditional Commands:\n") {
		t.Fatalf("expected all visible commands to be grouped, got:\n%s", help)
	}
	if strings.Contains(help, "\n  master ") {
		t.Fatalf("expected hidden master command to be omitted from help, got:\n%s", help)
	}
}

func TestVisibleRootCommandsBelongToGroups(t *testing.T) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	groupIDs := make(map[string]bool)
	for _, group := range rootCmd.Groups() {
		groupIDs[group.ID] = true
	}

	for _, cmd := range rootCmd.Commands() {
		if !cmd.IsAvailableCommand() && cmd.Name() != "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Fatalf("visible root command %q has no group", cmd.Name())
		}
		if !groupIDs[cmd.GroupID] {
			t.Fatalf("visible root command %q uses unknown group %q", cmd.Name(), cmd.GroupID)
		}
	}
}
