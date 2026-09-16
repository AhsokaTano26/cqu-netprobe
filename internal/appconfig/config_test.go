package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAndRelativeTokenFile(t *testing.T) {
	t.Setenv("CQU_NETPROBE_GATEWAY_URL", "")
	t.Setenv("CQU_NETPROBE_TOKEN", "")
	t.Setenv("CQU_NETPROBE_TOKEN_FILE", "")
	t.Setenv("CQU_NETPROBE_LOG_LEVEL", "")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "probe.token"), []byte("secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.json")
	data := []byte(`{"gateway_url":"https://gateway.example","token_file":"probe.token","log_level":"debug"}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "secret-token" {
		t.Fatalf("unexpected token %q", config.Token)
	}
	if config.LogLevel != "debug" {
		t.Fatalf("unexpected log level %q", config.LogLevel)
	}
}

func TestRejectsNonLoopbackHTTP(t *testing.T) {
	config := Config{GatewayURL: "http://gateway.example", Token: "secret", LogLevel: "info"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected HTTP gateway URL to be rejected")
	}
}
