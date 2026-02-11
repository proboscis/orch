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

func TestSanitizeUTF8Slice_ValidSliceReturnedAsIs(t *testing.T) {
	input := []string{"hello", "world"}
	got := sanitizeUTF8Slice(input)

	// Should return the same slice (no allocation) when all valid.
	if &got[0] != &input[0] {
		t.Fatal("sanitizeUTF8Slice() allocated a new slice for all-valid input")
	}
}

func TestSanitizeUTF8Map(t *testing.T) {
	invalid := string([]byte{'v', 0xff})
	input := map[string]string{"key": invalid, "ok": "fine"}
	got := sanitizeUTF8Map(input)

	for k, v := range got {
		if !utf8.ValidString(k) {
			t.Fatalf("sanitizeUTF8Map() key not valid UTF-8: %q", k)
		}
		if !utf8.ValidString(v) {
			t.Fatalf("sanitizeUTF8Map() value not valid UTF-8: %q", v)
		}
	}

	if got["key"] != "v\ufffd" {
		t.Fatalf("sanitizeUTF8Map()[\"key\"] = %q, want %q", got["key"], "v\ufffd")
	}
}

func TestSanitizeUTF8Map_ValidMapReturnedAsIs(t *testing.T) {
	input := map[string]string{"a": "b"}
	got := sanitizeUTF8Map(input)

	// Modify original and check got reflects it (same map).
	input["a"] = "changed"
	if got["a"] != "changed" {
		t.Fatal("sanitizeUTF8Map() allocated a new map for all-valid input")
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

func TestModelEventToProto_SanitizesInvalidFields(t *testing.T) {
	invalid := string([]byte{'e', 0xff, 'v'})
	event := &model.Event{
		Timestamp: time.Unix(1, 0),
		Type:      model.EventType(invalid),
		Name:      invalid,
		Attrs:     map[string]string{"key": invalid},
	}

	protoEvent := modelEventToProto(event)

	if !utf8.ValidString(protoEvent.Type) {
		t.Fatalf("modelEventToProto() Type not valid UTF-8: %q", protoEvent.Type)
	}
	if !utf8.ValidString(protoEvent.Name) {
		t.Fatalf("modelEventToProto() Name not valid UTF-8: %q", protoEvent.Name)
	}
	for k, v := range protoEvent.Attrs {
		if !utf8.ValidString(k) || !utf8.ValidString(v) {
			t.Fatalf("modelEventToProto() Attrs contains invalid UTF-8: key=%q val=%q", k, v)
		}
	}
}
