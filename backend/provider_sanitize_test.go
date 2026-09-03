package backend

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeProviderSource_hysteria2MissingObfsPassword(t *testing.T) {
	raw := []byte(`
proxies:
  - name: hy-bad
    type: hysteria2
    server: example.com
    port: 443
    password: main-pass
    obfs: salamander
  - name: hy-ok
    type: hysteria2
    server: example.com
    port: 444
    password: pass2
`)
	out, removed, err := SanitizeProviderSource(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected repair not remove, removed=%d", removed)
	}
	text := string(out)
	if !strings.Contains(text, "obfs-password: main-pass") || !strings.Contains(text, "hy-bad") {
		t.Fatalf("unexpected output: %s", text)
	}
}

func TestSanitizeProviderSource_dropUnrepairable(t *testing.T) {
	raw := []byte(`
proxies:
  - name: hy-bad
    type: hysteria2
    server: example.com
    port: 443
    obfs: salamander
  - name: ok-node
    type: vmess
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000001
`)
	out, removed, err := SanitizeProviderSource(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	text := string(out)
	if !strings.Contains(text, "ok-node") || strings.Contains(text, "hy-bad") {
		t.Fatalf("unexpected output: %s", text)
	}
}

func TestSanitizeProviderSource_base64Hysteria2ObfsNone(t *testing.T) {
	inner := "hysteria2://uuid@host:443?insecure=1&obfs=none&fastopen=1#test\nvmess://ok\n"
	raw := []byte(base64.StdEncoding.EncodeToString([]byte(inner)))
	out, removed, err := SanitizeProviderSource(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed=%d", removed)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	text := string(decoded)
	if strings.Contains(text, "obfs=none") {
		t.Fatalf("obfs=none not removed: %s", text)
	}
	if !strings.Contains(text, "vmess://ok") {
		t.Fatalf("missing vmess line: %s", text)
	}
}

func TestSanitizeProviderSource_dropPlaceholderSS(t *testing.T) {
	inner := "ss://bad@127.0.0.1:1080#placeholder\nvmess://ok\n"
	raw := []byte(base64.StdEncoding.EncodeToString([]byte(inner)))
	out, removed, err := SanitizeProviderSource(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	decoded, _ := base64.StdEncoding.DecodeString(string(out))
	if strings.Contains(string(decoded), "127.0.0.1") {
		t.Fatalf("placeholder not removed")
	}
}

func TestSanitizeProviderSource_dropLoopbackVmess(t *testing.T) {
	vmessCfg := `{"v":"2","ps":"请立即到官网下载新客户端！","add":"127.0.0.1","port":"443","id":"00000000-0000-0000-0000-000000000001","aid":"0","net":"tcp","type":"none"}`
	line := "vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessCfg)) + "\nvmess://ok\n"
	raw := []byte(base64.StdEncoding.EncodeToString([]byte(line)))
	out, removed, err := SanitizeProviderSource(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	decoded, _ := base64.StdEncoding.DecodeString(string(out))
	if strings.Contains(string(decoded), "127.0.0.1") {
		t.Fatalf("loopback vmess not removed: %s", decoded)
	}
}

func TestResanitizeProviderFile_writesRepair(t *testing.T) {
	dir := t.TempDir()
	id := "sub3"
	if err := os.MkdirAll(filepath.Join(dir, "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`
proxies:
  - name: hy-bad
    type: hysteria2
    server: example.com
    port: 443
    password: main-pass
    obfs: salamander
`)
	path := providerSourcePath(dir, id)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := ResanitizeProviderFile(dir, id)
	if err != nil {
		t.Fatalf("resanitize: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed=%d", removed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "obfs-password") {
		t.Fatalf("file not repaired: %s", string(data))
	}
}
