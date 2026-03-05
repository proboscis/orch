package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldAutoStartDaemonForCommand(t *testing.T) {
	root := &cobra.Command{Use: "orch"}
	run := &cobra.Command{Use: "run"}
	root.AddCommand(run)

	master := &cobra.Command{Use: "master"}
	start := &cobra.Command{Use: "start"}
	master.AddCommand(start)
	root.AddCommand(master)

	issue := &cobra.Command{Use: "issue"}
	list := &cobra.Command{Use: "list"}
	issue.AddCommand(list)
	root.AddCommand(issue)

	tests := []struct {
		name       string
		cmd        *cobra.Command
		remoteAddr string
		want       bool
	}{
		{name: "run local autostarts", cmd: run, remoteAddr: "", want: true},
		{name: "run remote skips autostart", cmd: run, remoteAddr: "zeus:7777", want: false},
		{name: "master start skips autostart", cmd: start, remoteAddr: "", want: false},
		{name: "master command skips autostart", cmd: master, remoteAddr: "", want: false},
		{name: "issue list keeps existing skip behavior", cmd: list, remoteAddr: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoStartDaemonForCommand(tt.cmd, tt.remoteAddr)
			if got != tt.want {
				t.Fatalf("shouldAutoStartDaemonForCommand(%q, remote=%q) = %v, want %v", tt.cmd.CommandPath(), tt.remoteAddr, got, tt.want)
			}
		})
	}
}
