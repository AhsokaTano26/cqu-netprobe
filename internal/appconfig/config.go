package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	GatewayURL string `json:"gateway_url"`
	Token      string `json:"token,omitempty"`
	TokenFile  string `json:"token_file,omitempty"`
	LogLevel   string `json:"log_level,omitempty"`
}

type Overrides struct {
	GatewayURL string
	TokenFile  string
	LogLevel   string
}

func Load(path string, overrides Overrides) (Config, error) {
	config := Config{LogLevel: "info"}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return Config{}, fmt.Errorf("decode config file: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err == nil {
			return Config{}, errors.New("decode config file: trailing JSON data")
		} else if !errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("decode config file: %w", err)
		}
		if config.TokenFile != "" && !filepath.IsAbs(config.TokenFile) {
			config.TokenFile = filepath.Join(filepath.Dir(path), config.TokenFile)
		}
	}

	applyEnvironment(&config)
	if overrides.GatewayURL != "" {
		config.GatewayURL = overrides.GatewayURL
	}
	if overrides.TokenFile != "" {
		config.TokenFile = overrides.TokenFile
		config.Token = ""
	}
	if overrides.LogLevel != "" {
		config.LogLevel = overrides.LogLevel
	}

	if config.TokenFile != "" {
		data, err := os.ReadFile(config.TokenFile)
		if err != nil {
			return Config{}, fmt.Errorf("read token file: %w", err)
		}
		config.Token = strings.TrimSpace(string(data))
	}
	config.GatewayURL = strings.TrimRight(strings.TrimSpace(config.GatewayURL), "/")
	config.Token = strings.TrimSpace(config.Token)
	config.LogLevel = strings.ToLower(strings.TrimSpace(config.LogLevel))
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func applyEnvironment(config *Config) {
	if value := os.Getenv("CQU_NETPROBE_GATEWAY_URL"); value != "" {
		config.GatewayURL = value
	}
	if value := os.Getenv("CQU_NETPROBE_TOKEN"); value != "" {
		config.Token = value
		config.TokenFile = ""
	}
	if value := os.Getenv("CQU_NETPROBE_TOKEN_FILE"); value != "" {
		config.TokenFile = value
		config.Token = ""
	}
	if value := os.Getenv("CQU_NETPROBE_LOG_LEVEL"); value != "" {
		config.LogLevel = value
	}
}

func (c Config) Validate() error {
	if c.GatewayURL == "" {
		return errors.New("gateway URL is required")
	}
	parsed, err := url.Parse(c.GatewayURL)
	if err != nil || parsed.Host == "" {
		return errors.New("gateway URL must be an absolute URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("gateway URL must not contain credentials, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("gateway URL must not contain a path")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || !isLoopbackHost(host) {
			return errors.New("gateway URL must use HTTPS (HTTP is allowed only for loopback development)")
		}
	}
	if c.Token == "" {
		return errors.New("gateway token is required")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error", "off":
	default:
		return fmt.Errorf("unsupported log level %q", c.LogLevel)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
