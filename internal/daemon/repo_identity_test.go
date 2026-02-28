package daemon

import (
	"strings"
	"testing"
)

func TestRepoIDTokenEncodeDecode(t *testing.T) {
	token := encodeRepoIDToken("owner-repo")
	if token != "repoid:owner-repo" {
		t.Fatalf("encodeRepoIDToken() = %q, want %q", token, "repoid:owner-repo")
	}

	id, ok := decodeRepoIDToken(token)
	if !ok {
		t.Fatal("decodeRepoIDToken() expected ok=true")
	}
	if id != "owner-repo" {
		t.Fatalf("decoded id = %q, want %q", id, "owner-repo")
	}

	if _, ok := decodeRepoIDToken("/tmp/issues"); ok {
		t.Fatal("decodeRepoIDToken() expected ok=false for non-token input")
	}
}

func TestNewProtoClientWithAddressRemoteUsesRepoIDToken(t *testing.T) {
	client := NewProtoClientWithAddress("/tmp/project", "/tmp/issues", "zeus:7777")

	if !strings.HasPrefix(client.projectRoot, "repoid:") {
		t.Fatalf("projectRoot = %q, want repoid token", client.projectRoot)
	}
	if !strings.HasPrefix(client.issuesRoot, "repoid:") {
		t.Fatalf("issuesRoot = %q, want repoid token", client.issuesRoot)
	}
	if client.projectRoot != client.issuesRoot {
		t.Fatalf("projectRoot token (%q) != issuesRoot token (%q)", client.projectRoot, client.issuesRoot)
	}
}

func TestProjectRootForRequestRemoteEncodesPath(t *testing.T) {
	client := NewProtoClientWithAddress("/tmp/project", "/tmp/issues", "zeus:7777")

	encoded := client.projectRootForRequest("/tmp/other-project")
	if !strings.HasPrefix(encoded, "repoid:") {
		t.Fatalf("projectRootForRequest(path) = %q, want repoid token", encoded)
	}

	passthrough := client.projectRootForRequest(client.projectRoot)
	if passthrough != client.projectRoot {
		t.Fatalf("projectRootForRequest(token) = %q, want %q", passthrough, client.projectRoot)
	}
}

func TestNewProtoClientWithAddressRemotePreservesRepoIDToken(t *testing.T) {
	client := NewProtoClientWithAddress("repoid:server-repo", "", "zeus:7777")

	if client.projectRoot != "repoid:server-repo" {
		t.Fatalf("projectRoot = %q, want %q", client.projectRoot, "repoid:server-repo")
	}
	if client.issuesRoot != "repoid:server-repo" {
		t.Fatalf("issuesRoot = %q, want %q", client.issuesRoot, "repoid:server-repo")
	}
}
