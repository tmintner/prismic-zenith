package guiassets

import (
	"strings"
	"testing"
)

func TestComposeHTML_ContainsInjectedAssets(t *testing.T) {
	html := ComposeHTML()

	if html == "" {
		t.Fatal("ComposeHTML returned empty string")
	}

	// CSS should be injected (placeholder replaced)
	if strings.Contains(html, "/* INJECT_CSS */") {
		t.Error("CSS placeholder was not replaced")
	}
	// JS should be injected (placeholder replaced)
	if strings.Contains(html, "/* INJECT_JS */") {
		t.Error("JS placeholder was not replaced")
	}

	// Verify actual CSS content is present
	if !strings.Contains(html, "var(--bg)") {
		t.Error("CSS variables not found in composed HTML")
	}
	// Verify actual JS content is present
	if !strings.Contains(html, "DOMContentLoaded") {
		t.Error("JS event listener not found in composed HTML")
	}
	// Structural HTML elements
	for _, want := range []string{"<div class=\"dashboard\">", "chat-messages", "btn-send", "btn-recommend"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q in composed HTML", want)
		}
	}
}

func TestRawAssets_NotEmpty(t *testing.T) {
	if htmlData == "" {
		t.Error("htmlData is empty")
	}
	if cssData == "" {
		t.Error("cssData is empty")
	}
	if jsData == "" {
		t.Error("jsData is empty")
	}
}
