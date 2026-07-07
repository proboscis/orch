package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/proboscis/orch/internal/orchapi"
	buildversion "github.com/proboscis/orch/internal/version"
	"github.com/spf13/cobra"
)

type apiVersionPinger interface {
	PingStatus(context.Context) (*orchapi.PingStatus, error)
}

var daemonVersionMismatchWarned bool

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print orch version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			printVersion()
			return nil
		},
	}
}

func printVersion() {
	fmt.Printf("version %s\n", buildversion.Version)
	fmt.Printf("commit %s\n", buildversion.Commit)
	fmt.Printf("build_date %s\n", buildversion.BuildDate)
	fmt.Printf("goos/goarch %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func pingAPIWithVersionCheck(ctx context.Context, client apiVersionPinger) error {
	resp, err := client.PingStatus(ctx)
	if err != nil {
		return err
	}
	warnIfDaemonVersionMismatch(resp.Version)
	return nil
}

func warnIfDaemonVersionMismatch(daemonVersion string) {
	if daemonVersionMismatchWarned {
		return
	}

	cliVersion := strings.TrimSpace(buildversion.Version)
	daemonVersion = strings.TrimSpace(daemonVersion)
	if cliVersion == daemonVersion {
		return
	}

	if cliVersion == "" {
		cliVersion = "unknown"
	}
	if daemonVersion == "" {
		daemonVersion = "unknown"
	}

	fmt.Fprintf(os.Stderr, "warning: orch CLI %s / daemon %s version mismatch - run 'orch daemon-restart'\n", cliVersion, daemonVersion)
	daemonVersionMismatchWarned = true
}
