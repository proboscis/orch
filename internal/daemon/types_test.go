package daemon

import "testing"

func TestFileURI(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", ""},
		{"/path/to/file.md", "file:///path/to/file.md"},
		{"/Users/test/vault/issues/orch-123.md", "file:///Users/test/vault/issues/orch-123.md"},
		{"https://github.com/owner/repo/issues/123", "https://github.com/owner/repo/issues/123"},
		{"http://example.com/page", "http://example.com/page"},
		{"https://api.github.com/repos/test", "https://api.github.com/repos/test"},
	}

	for _, tt := range tests {
		got := FileURI(tt.path)
		if got != tt.want {
			t.Errorf("FileURI(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestEncodeCursor(t *testing.T) {
	cursor := EncodeCursor(42)
	if cursor == "" {
		t.Error("EncodeCursor returned empty string")
	}

	offset, err := DecodeCursor(cursor)
	if err != nil {
		t.Errorf("DecodeCursor failed: %v", err)
	}
	if offset != 42 {
		t.Errorf("DecodeCursor = %d, want 42", offset)
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	offset, err := DecodeCursor("")
	if err != nil {
		t.Errorf("DecodeCursor empty should not error: %v", err)
	}
	if offset != 0 {
		t.Errorf("DecodeCursor empty = %d, want 0", offset)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Error("DecodeCursor should fail for invalid base64")
	}
}

func TestDecodeCursorNegative(t *testing.T) {
	cursor := EncodeCursor(-5)
	_, err := DecodeCursor(cursor)
	if err == nil {
		t.Error("DecodeCursor should fail for negative offset")
	}
}
