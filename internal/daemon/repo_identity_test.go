package daemon

import "testing"

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

func TestNewProtoClientWithAddressRemoteUsesPlainProjectID(t *testing.T) {
	projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/remote-project.git")
	client := NewProtoClientWithAddress(projectRoot, "zeus:7777")

	if client.projectRoot != "example-remote-project" {
		t.Fatalf("projectRoot = %q, want %q", client.projectRoot, "example-remote-project")
	}
}

func TestProjectRootForRequestRemoteOmitsCompatibilityField(t *testing.T) {
	client := NewProtoClientWithAddress("/tmp/project", "zeus:7777")

	encoded := client.projectRootForRequest("/tmp/other-project")
	if encoded != "" {
		t.Fatalf("projectRootForRequest(path) = %q, want empty", encoded)
	}

	passthrough := client.projectRootForRequest(client.projectRoot)
	if passthrough != "" {
		t.Fatalf("projectRootForRequest(token) = %q, want empty", passthrough)
	}
}

func TestNewProtoClientWithAddressRemoteClearsUnknownPathIdentity(t *testing.T) {
	client := NewProtoClientWithAddress("/tmp/project", "zeus:7777")

	if client.projectRoot != "" {
		t.Fatalf("projectRoot = %q, want empty", client.projectRoot)
	}
	if got := client.projectIDForRequest("/tmp/project"); got != "" {
		t.Fatalf("projectIDForRequest(path) = %q, want empty", got)
	}
	if ctx := client.requestContext("/tmp/project"); ctx != nil {
		t.Fatalf("requestContext(path) = %#v, want nil", ctx)
	}
}

func TestProjectIDForRequestRemoteUsesStoredProjectID(t *testing.T) {
	client := NewProtoClientWithAddress("server-repo", "zeus:7777")

	if got := client.projectIDForRequest(""); got != "server-repo" {
		t.Fatalf("projectIDForRequest(\"\") = %q, want %q", got, "server-repo")
	}
}

func TestNewProtoClientWithAddressRemotePreservesPlainProjectID(t *testing.T) {
	client := NewProtoClientWithAddress("server-repo", "zeus:7777")

	if client.projectRoot != "server-repo" {
		t.Fatalf("projectRoot = %q, want %q", client.projectRoot, "server-repo")
	}
}
