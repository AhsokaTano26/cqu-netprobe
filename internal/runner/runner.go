package runner

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/probe"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

type Sink interface {
	Push(context.Context, protocol.PushRequest) error
}

type Runner struct {
	// StopOnPushError makes local file write failures terminate the run.
	StopOnPushError bool
	client          Sink
	targets         protocol.TargetList
	probeVersion    string
	logger          *slog.Logger
}

type measurement struct {
	targetID string
	typeName string
	icmp     *protocol.ICMPResult
	http     *protocol.HTTPResult
	err      error
}

func New(client Sink, targets protocol.TargetList, probeVersion string, logger *slog.Logger) *Runner {
	return &Runner{client: client, targets: targets, probeVersion: probeVersion, logger: logger}
}

func (r *Runner) Run(ctx context.Context) error {
	interval := protocol.Milliseconds(r.targets.Config.IntervalMS)
	nextStart := time.Now()
	for {
		if err := waitUntil(ctx, nextStart); err != nil {
			return err
		}
		runStart := time.Now()
		if err := r.runRound(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		nextStart = nextStart.Add(interval)
		now := time.Now()
		if !nextStart.After(now) {
			skipped := now.Sub(nextStart)/interval + 1
			nextStart = nextStart.Add(skipped * interval)
			r.logger.Warn("measurement round exceeded its schedule; skipped ticks",
				"duration", time.Since(runStart), "skipped", skipped)
		}
	}
}

func (r *Runner) runRound(ctx context.Context) error {
	roundStart := time.Now()
	measurements := r.measure(ctx)
	results := make(map[string]protocol.TargetMeasurements)
	for item := range measurements {
		if item.err != nil {
			r.logger.Warn("measurement failed", "target", item.targetID, "type", item.typeName, "err", item.err)
		}
		result := results[item.targetID]
		switch item.typeName {
		case "icmp":
			result.ICMP = item.icmp
		case "http":
			result.HTTP = item.http
		}
		results[item.targetID] = result
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(results) == 0 {
		r.logger.Debug("no supported targets configured; skipping push")
		return nil
	}
	payload := protocol.PushRequest{
		Version:      protocol.Version,
		Timestamp:    time.Now().Unix(),
		ProbeVersion: r.probeVersion,
		Results:      results,
	}
	if err := r.client.Push(ctx, payload); err != nil {
		if r.StopOnPushError {
			return err
		}
		if !errors.Is(err, context.Canceled) {
			r.logger.Error("push failed", "err", err)
		}
		return nil
	}
	r.logger.Info("measurement round pushed", "targets", len(results), "duration", time.Since(roundStart))
	return nil
}

func (r *Runner) measure(ctx context.Context) <-chan measurement {
	count := 0
	for _, target := range r.targets.Targets {
		for _, probeType := range target.ProbeTypes {
			if probeType == "icmp" || probeType == "http" {
				count++
			}
		}
	}
	output := make(chan measurement, count)
	var group sync.WaitGroup
	for _, target := range r.targets.Targets {
		for _, probeType := range target.ProbeTypes {
			target, probeType := target, probeType
			switch probeType {
			case "icmp":
				group.Add(1)
				go func() {
					defer group.Done()
					result, err := probe.ICMP(ctx, target.Address, r.targets.Config.ICMP)
					output <- measurement{targetID: target.TargetID, typeName: probeType, icmp: &result, err: err}
				}()
			case "http":
				group.Add(1)
				go func() {
					defer group.Done()
					result, err := probe.HTTP(ctx, target.Address, r.targets.Config.HTTP)
					output <- measurement{targetID: target.TargetID, typeName: probeType, http: &result, err: err}
				}()
			case "dns":
				r.logger.Warn("DNS target ignored because DNS probing is disabled", "target", target.TargetID)
			}
		}
	}
	go func() {
		group.Wait()
		close(output)
	}()
	return output
}

func waitUntil(ctx context.Context, deadline time.Time) error {
	delay := time.Until(deadline)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
