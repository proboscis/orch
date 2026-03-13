package daemon

import (
	"os"
	"strings"
)

var currentHostname = os.Hostname

func HostWorkerID(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}

	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	idHost := strings.Trim(b.String(), "-")
	if idHost == "" {
		idHost = "localhost"
	}
	return "host-" + idHost
}

func defaultWorkerID() string {
	host, _ := currentHostname()
	return HostWorkerID(host)
}
