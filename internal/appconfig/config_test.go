package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndPrecedence(t *testing.T) {
	for _, key := range []string{"CQU_NETPROBE_GATEWAY_URL", "CQU_NETPROBE_TOKEN", "CQU_NETPROBE_LOG_LEVEL"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	directory := t.TempDir()
	t.Chdir(directory)
	configPath := filepath.Join(directory, "config.json")
	data := []byte(`{"gateway_url":"https://gateway.example","token":"file-token","log_level":"debug"}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load("", Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "file-token" || config.GatewayURL != "https://gateway.example" {
		t.Fatal("default file not loaded")
	}
	if config.LogLevel != "debug" {
		t.Fatalf("unexpected log level %q", config.LogLevel)
	}
	t.Setenv("CQU_NETPROBE_GATEWAY_URL", "https://env.example")
	t.Setenv("CQU_NETPROBE_TOKEN", "env-token")
	t.Setenv("CQU_NETPROBE_LOG_LEVEL", "warn")
	config, err = Load("", Overrides{})
	if err != nil || config.Token != "env-token" || config.GatewayURL != "https://env.example" || config.LogLevel != "warn" {
		t.Fatalf("environment precedence failed: %v", err)
	}
	url, token, level := "https://cli.example", "cli-token", "off"
	config, err = Load("", Overrides{GatewayURL: &url, Token: &token, LogLevel: &level})
	if err != nil || config.Token != token || config.GatewayURL != url || config.LogLevel != level {
		t.Fatalf("CLI precedence failed: %v", err)
	}
	token = ""
	if _, err := Load("", Overrides{Token: &token}); err == nil {
		t.Fatal("explicit empty CLI token must override lower-priority values")
	}
	t.Setenv("CQU_NETPROBE_TOKEN", "")
	if _, err := Load("", Overrides{}); err == nil {
		t.Fatal("explicit empty environment token must override file")
	}
}

func TestMissingConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CQU_NETPROBE_GATEWAY_URL", "https://env.example")
	t.Setenv("CQU_NETPROBE_TOKEN", "env-token")
	t.Setenv("CQU_NETPROBE_LOG_LEVEL", "info")
	if _, err := Load("", Overrides{}); err != nil {
		t.Fatalf("environment-only startup failed: %v", err)
	}
	if _, err := Load("missing.json", Overrides{}); err == nil {
		t.Fatal("explicit missing file must fail")
	}
}

func TestLocalConfigWithoutCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CQU_NETPROBE_GATEWAY_URL", "")
	t.Setenv("CQU_NETPROBE_TOKEN", "")
	t.Setenv("CQU_NETPROBE_LOG_LEVEL", "debug")
	t.Setenv("CQU_NETPROBE_LOCAL_INPUT", "targets.json")
	t.Setenv("CQU_NETPROBE_LOCAL_OUTPUT", "results.jsonl")
	c, err := Load("", Overrides{})
	if err != nil || c.LogLevel != "debug" {
		t.Fatalf("local config failed: %v", err)
	}
	t.Setenv("CQU_NETPROBE_LOG_LEVEL", "invalid")
	if _, err := Load("", Overrides{}); err == nil {
		t.Fatal("invalid local logging config accepted")
	}
}

func TestLocalConfigPrecedenceAndPairValidation(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	data := []byte(`{"local_input":"file-input.json","local_output":"file-output.jsonl"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CQU_NETPROBE_GATEWAY_URL", "CQU_NETPROBE_TOKEN", "CQU_NETPROBE_LOG_LEVEL", "CQU_NETPROBE_LOCAL_INPUT", "CQU_NETPROBE_LOCAL_OUTPUT"} {
		os.Unsetenv(key)
	}
	t.Setenv("CQU_NETPROBE_LOCAL_INPUT", "env-input.json")
	t.Setenv("CQU_NETPROBE_LOCAL_OUTPUT", "env-output.jsonl")
	input, output := "cli-input.json", "cli-output.jsonl"
	c, err := Load(path, Overrides{LocalInput: &input, LocalOutput: &output})
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalInput != input || c.LocalOutput != output {
		t.Fatalf("CLI local overrides failed: %#v", c)
	}
	empty := ""
	if _, err := Load(path, Overrides{LocalOutput: &empty}); err == nil {
		t.Fatal("accepted an unpaired local input")
	}
}

func TestRejectsNonLoopbackHTTP(t *testing.T) {
	config := Config{GatewayURL: "http://gateway.example", Token: "secret", LogLevel: "info"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected HTTP gateway URL to be rejected")
	}
}
