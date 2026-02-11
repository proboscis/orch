package daemon

import (
	"reflect"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/s22625/orch/internal/model"
)

func TestSanitizeUTF8_ValidStringUnchanged(t *testing.T) {
	input := "hello-world"
	if got := sanitizeUTF8(input); got != input {
		t.Fatalf("sanitizeUTF8() = %q, want %q", got, input)
	}
}

func TestSanitizeUTF8_InvalidStringBecomesValid(t *testing.T) {
	input := string([]byte{'a', 0xff, 'b'})
	got := sanitizeUTF8(input)

	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeUTF8() produced invalid UTF-8: %q", got)
	}

	if got != "a\ufffdb" {
		t.Fatalf("sanitizeUTF8() = %q, want %q", got, "a\ufffdb")
	}
}

func TestSanitizeUTF8Slice(t *testing.T) {
	input := []string{"ok", string([]byte{'x', 0xff, 'y'})}
	got := sanitizeUTF8Slice(input)
	want := []string{"ok", "x\ufffdy"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitizeUTF8Slice() = %#v, want %#v", got, want)
	}
}

func TestModelIssueToProto_SanitizesInvalidTextFields(t *testing.T) {
	invalid := string([]byte{'z', 0xff, 'z'})
	issue := &model.Issue{
		ID:         "orch-utf8",
		Title:      invalid,
		Summary:    invalid,
		Body:       invalid,
		Topic:      invalid,
		Tags:       []string{"good", invalid},
		Status:     model.IssueStatusOpen,
		Path:       invalid,
		ModifiedAt: time.Unix(1, 0),
	}

	protoIssue := modelIssueToProto(issue)

	if !utf8.ValidString(protoIssue.Title) || !utf8.ValidString(protoIssue.Summary) || !utf8.ValidString(protoIssue.Body) || !utf8.ValidString(protoIssue.Topic) {
		t.Fatalf("modelIssueToProto() returned invalid UTF-8 in text fields")
	}

	for _, tag := range protoIssue.Tags {
		if !utf8.ValidString(tag) {
			t.Fatalf("modelIssueToProto() returned invalid UTF-8 tag: %q", tag)
		}
	}
}
