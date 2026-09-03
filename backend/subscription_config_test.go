package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEnsureNestedProxyGroups_structure(t *testing.T) {
	cfg := map[string]any{
		"proxy-groups": []any{
			map[string]any{
				"name": "PROXY",
				"type": "select",
				"use":  []any{"sub2"},
			},
		},
	}
	items := []Subscription{
		{ID: "sub2", URL: "https://a.example/sub", Enabled: true},
		{ID: "sub3", URL: "https://b.example/sub", Enabled: false},
	}
	if err := ensureNestedProxyGroups(cfg, items); err != nil {
		t.Fatalf("ensureNestedProxyGroups: %v", err)
	}
	groups, ok := cfg["proxy-groups"].([]any)
	if !ok || len(groups) < 3 {
		t.Fatalf("expected PROXY + 2 child groups, got %v", cfg["proxy-groups"])
	}
	proxy := groups[0].(map[string]any)
	proxies, _ := proxy["proxies"].([]any)
	if len(proxies) != 3 {
		t.Fatalf("PROXY proxies want 3 (SUB_sub2,SUB_sub3,DIRECT), got %d", len(proxies))
	}
	if _, hasUse := proxy["use"]; hasUse {
		t.Fatal("PROXY should not use direct provider refs in nested mode")
	}
	sub2 := groups[1].(map[string]any)
	if sub2["name"] != SubscriptionGroupName("sub2") {
		t.Fatalf("unexpected child group: %v", sub2["name"])
	}
	use, _ := sub2["use"].([]any)
	if len(use) != 1 || use[0] != "sub2" {
		t.Fatalf("sub2 group use: %v", use)
	}
}

func TestSyncProvidersConfig_registersAllSubscriptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
proxy-groups:
  - name: PROXY
    type: select
    proxies: [DIRECT]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []Subscription{
		{ID: "sub2", URL: "https://a.example/sub", Enabled: true},
		{ID: "sub3", URL: "https://b.example/sub", Enabled: false},
	}
	if err := persistSubscriptions(dir, items); err != nil {
		t.Fatalf("persist: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "sub2:") || !strings.Contains(text, "sub3:") {
		t.Fatalf("both providers should be registered:\n%s", text)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	if len(providers) != 2 {
		t.Fatalf("provider count=%d", len(providers))
	}
}
