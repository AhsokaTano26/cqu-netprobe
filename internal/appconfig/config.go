package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	GatewayURL  string `json:"gateway_url"`
	Token       string `json:"token,omitempty"`
	LogLevel    string `json:"log_level,omitempty"`
	LocalInput  string `json:"local_input,omitempty"`
	LocalOutput string `json:"local_output,omitempty"`
}

type Overrides struct {
	GatewayURL  *string
	Token       *string
	LogLevel    *string
	LocalInput  *string
	LocalOutput *string
}

func Load(path string, overrides Overrides) (Config, error) {
	config := Config{LogLevel: "info"}
	defaultPath := path == ""
	if defaultPath {
		path = "config.json"
	}
	{
		data, err := os.ReadFile(path)
		if err != nil && !(defaultPath && errors.Is(err, os.ErrNotExist)) {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		if err == nil {
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
		}
	}

	applyEnvironment(&config)
	if overrides.GatewayURL != nil {
		config.GatewayURL = *overrides.GatewayURL
	}
	if overrides.Token != nil {
		config.Token = *overrides.Token
	}
	if overrides.LogLevel != nil {
		config.LogLevel = *overrides.LogLevel
	}
	if overrides.LocalInput != nil {
		config.LocalInput = *overrides.LocalInput
	}
	if overrides.LocalOutput != nil {
		config.LocalOutput = *overrides.LocalOutput
	}
	config.GatewayURL = strings.TrimRight(strings.TrimSpace(config.GatewayURL), "/")
	config.Token = strings.TrimSpace(config.Token)
	config.LogLevel = strings.ToLower(strings.TrimSpace(config.LogLevel))
	config.LocalInput = strings.TrimSpace(config.LocalInput)
	config.LocalOutput = strings.TrimSpace(config.LocalOutput)
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func applyEnvironment(config *Config) {
	if value, ok := os.LookupEnv("CQU_NETPROBE_GATEWAY_URL"); ok {
		config.GatewayURL = value
	}
	if value, ok := os.LookupEnv("CQU_NETPROBE_TOKEN"); ok {
		config.Token = value
	}
	if value, ok := os.LookupEnv("CQU_NETPROBE_LOG_LEVEL"); ok {
		config.LogLevel = value
	}
	if value, ok := os.LookupEnv("CQU_NETPROBE_LOCAL_INPUT"); ok {
		config.LocalInput = value
	}
	if value, ok := os.LookupEnv("CQU_NETPROBE_LOCAL_OUTPUT"); ok {
		config.LocalOutput = value
	}
}

func (c Config) Validate() error {
	if err := c.validateLogLevel(); err != nil {
		return err
	}
	if (c.LocalInput == "") != (c.LocalOutput == "") {
		return errors.New("local_input and local_output must be configured together")
	}
	if c.LocalInput != "" {
		return nil
	}
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
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || !isLoopbackHost(host) {
			return errors.New("gateway URL must use HTTPS (HTTP is allowed only for loopback development)")
		}
	}
	if c.Token == "" {
		return errors.New("gateway token is required")
	}
	return nil
}

func (c Config) validateLogLevel() error {
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
