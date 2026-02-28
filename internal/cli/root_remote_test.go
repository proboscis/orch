package cli

import (
	"testing"

	"github.com/s22625/orch/internal/config"
)

func TestResolveRemoteAddrPrecedence(t *testing.T) {
	clientCfg := &config.ClientConfig{
		Remote: config.ClientRemoteConfig{
			Default: "zeus",
			Hosts: map[string]config.ClientRemoteHost{
				"zeus":  {Addr: "zeus:7777"},
				"cloud": {Addr: "10.0.0.5:7777"},
			},
		},
	}

	tests := []struct {
		name        string
		flagValue   string
		flagChanged bool
		envValue    string
		want        string
	}{
		{
			name:        "explicit empty flag forces local",
			flagValue:   "",
			flagChanged: true,
			envValue:    "cloud",
			want:        "",
		},
		{
			name:        "flag alias resolves via hosts",
			flagValue:   "cloud",
			flagChanged: true,
			envValue:    "",
			want:        "10.0.0.5:7777",
		},
		{
			name:        "env alias resolves when flag not set",
			flagValue:   "",
			flagChanged: false,
			envValue:    "zeus",
			want:        "zeus:7777",
		},
		{
			name:        "client default applies when no flag or env",
			flagValue:   "",
			flagChanged: false,
			envValue:    "",
			want:        "zeus:7777",
		},
		{
			name:        "raw address passes through",
			flagValue:   "",
			flagChanged: false,
			envValue:    "host.example:9999",
			want:        "host.example:9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRemoteAddr(tt.flagValue, tt.flagChanged, tt.envValue, clientCfg)
			if got != tt.want {
				t.Fatalf("resolveRemoteAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}
