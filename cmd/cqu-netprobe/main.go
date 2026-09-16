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
	configFile := flag.String("config.file", "", "path to a JSON configuration file")
	gatewayURL := flag.String("gateway.url", "", "Gateway base URL (overrides configuration and environment)")
	tokenFile := flag.String("gateway.token-file", "", "path to the Gateway token (overrides configuration and environment)")
	logLevel := flag.String("log.level", "", "debug, info, warn, or error")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if len(version) == 0 || len(version) > 32 {
		return errors.New("build version must contain 1 to 32 bytes")
	}

	config, err := appconfig.Load(*configFile, appconfig.Overrides{
		GatewayURL: *gatewayURL,
		TokenFile:  *tokenFile,
		LogLevel:   *logLevel,
	})
	if err != nil {
		return err
	}
	logOutput := io.Writer(os.Stderr)
	if config.LogLevel == "off" {
		logOutput = io.Discard
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: parseLogLevel(config.LogLevel)}))
	client, err := gateway.NewClient(config.GatewayURL, config.Token, version)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("starting cqu-netprobe", "version", version, "gateway", config.GatewayURL)
	targets, err := fetchTargetsWithRetry(ctx, client, logger)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("shutdown complete")
			return nil
		}
		return err
	}
	logger.Info("target configuration loaded", "targets", len(targets.Targets), "interval", protocol.Milliseconds(targets.Config.IntervalMS))

	probeRunner := runner.New(client, targets, version, logger)
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
