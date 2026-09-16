package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/appconfig"
	"github.com/AhsokaTano26/cqu-netprobe/internal/gateway"
	"github.com/AhsokaTano26/cqu-netprobe/internal/localio"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
	"github.com/AhsokaTano26/cqu-netprobe/internal/runner"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cqu-netprobe:", err)
		os.Exit(1)
	}
}

func run() error {
	configFile := flag.String("config-file", "", "JSON configuration file (default: ./config.json; env: CQU_NETPROBE_CONFIG_FILE)")
	gatewayURL := flag.String("gateway-url", "", "Gateway base URL (overrides environment and config)")
	token := flag.String("token", "", "Gateway token (overrides environment and config)")
	logLevel := flag.String("log-level", "", "debug, info, warn, error, or off (overrides environment and config)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	localInput := flag.String("local-input", "", "local Gateway target configuration JSON (requires --local-output)")
	localOutput := flag.String("local-output", "", "append measurements to this JSONL file (requires --local-input)")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if len(version) == 0 || len(version) > 32 {
		return errors.New("build version must contain 1 to 32 bytes")
	}

	overrides := appconfig.Overrides{}
	path := os.Getenv("CQU_NETPROBE_CONFIG_FILE")
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config-file":
			path = *configFile
		case "gateway-url":
			overrides.GatewayURL = gatewayURL
		case "token":
			overrides.Token = token
		case "log-level":
			overrides.LogLevel = logLevel
		case "local-input":
			overrides.LocalInput = localInput
		case "local-output":
			overrides.LocalOutput = localOutput
		}
	})
	config, err := appconfig.Load(path, overrides)
	if err != nil {
		return err
	}
	localMode := config.LocalInput != ""
	logOutput := io.Writer(os.Stderr)
	if config.LogLevel == "off" {
		logOutput = io.Discard
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: parseLogLevel(config.LogLevel)}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if localMode {
		targets, output, err := localio.Open(config.LocalInput, config.LocalOutput)
		if err != nil {
			return err
		}
		logger.Info("starting local test", "input", config.LocalInput, "output", config.LocalOutput, "targets", len(targets.Targets))
		localRunner := runner.New(output, targets, version, logger)
		localRunner.StopOnPushError = true
		runErr := localRunner.Run(ctx)
		closeErr := output.Close()
		if errors.Is(runErr, context.Canceled) {
			runErr = nil
		}
		return errors.Join(runErr, closeErr)
	}
	client, err := gateway.NewClient(config.GatewayURL, config.Token, version)
	if err != nil {
		return err
	}

	logger.Info("starting cqu-netprobe", "version", version, "gateway", config.GatewayURL)
	targets, err := fetchTargetsWithRetry(ctx, client, logger)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("shutdown complete")
			return nil
		}
		return err
	}
	logger.Info("target configuration loaded", "config_id", targets.ConfigID, "targets", len(targets.Targets), "interval", protocol.Milliseconds(targets.Config.IntervalMS))

	probeRunner := runner.NewManaged(client, targets, version, logger)
	if err := probeRunner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

func fetchTargetsWithRetry(ctx context.Context, client *gateway.Client, logger *slog.Logger) (protocol.TargetList, error) {
	delay := time.Second
	for {
		targets, err := client.FetchTargets(ctx)
		if err == nil {
			return targets, nil
		}
		if errors.Is(err, context.Canceled) {
			return protocol.TargetList{}, err
		}
		logger.Error("target configuration fetch failed; retrying", "err", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.TargetList{}, ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, time.Minute)
	}
}

func parseLogLevel(value string) slog.Level {
	switch value {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "off":
		return slog.Level(100)
	default:
		return slog.LevelInfo
	}
}
