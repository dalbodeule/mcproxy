package store

import (
	"testing"

	"go.minekube.com/common/minecraft/component"
)

func TestSanitizeAuditValueRemovesUnsafeMinecraftComponents(t *testing.T) {
	got := sanitizeAuditValue(map[string]any{
		"reason": &component.Text{Content: "Unable to authenticate"},
		"nested": []any{&component.Translation{Key: "disconnect.genericReason"}},
	})

	details, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("sanitized value type = %T, want map[string]any", got)
	}
	if details["reason"] != "Unable to authenticate" {
		t.Fatalf("reason = %#v", details["reason"])
	}
	nested, ok := details["nested"].([]any)
	if !ok || len(nested) != 1 || nested[0] != "disconnect.genericReason" {
		t.Fatalf("nested = %#v", details["nested"])
	}
}
