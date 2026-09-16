package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/gateway"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestRunnerMeasuresAndPushesImmediately(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer targetServer.Close()

	pushed := make(chan protocol.PushRequest, 1)
	gatewayServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/push" {
			http.NotFound(response, request)
			return
		}
		var payload protocol.PushRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		pushed <- payload
		response.WriteHeader(http.StatusNoContent)
	}))
	defer gatewayServer.Close()

	client, err := gateway.NewClient(gatewayServer.URL, "test-token", "test")
	if err != nil {
		t.Fatal(err)
	}
	targets := protocol.TargetList{
		Version:  protocol.Version,
		ConfigID: "23ea604f-6e47-5710-bc10-ab9b6a1302a3",
		Config: protocol.MeasurementConfig{
			IntervalMS: 10_000,
			ICMP:       protocol.ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 100},
			HTTP:       protocol.HTTPConfig{Method: "GET", FollowRedirects: true, VerifyTLS: true, TimeoutMS: 1_000},
		},
		Targets: []protocol.Target{{TargetID: "test_http", Address: targetServer.URL, ProbeTypes: []string{"http"}}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := New(client, targets, "test", logger)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	select {
	case payload := <-pushed:
		if payload.ConfigID != targets.ConfigID {
			t.Fatalf("unexpected config_id %q", payload.ConfigID)
		}
		measurement := payload.Results["test_http"].HTTP
		if measurement == nil || !measurement.Success || measurement.StatusCode == nil || *measurement.StatusCode != http.StatusNoContent {
			t.Fatalf("unexpected measurement: %#v", measurement)
		}
		cancel()
	case <-ctx.Done():
		t.Fatal("runner did not push its first round immediately")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected runner error: %v", err)
	}
}

type refreshingClient struct {
	pushes  []protocol.PushRequest
	targets protocol.TargetList
	fetches int
}

func (c *refreshingClient) Push(_ context.Context, payload protocol.PushRequest) error {
	c.pushes = append(c.pushes, payload)
	if len(c.pushes) == 1 {
		return protocol.ErrConfigStale
	}
	return nil
}

func (c *refreshingClient) FetchTargets(_ context.Context) (protocol.TargetList, error) {
	c.fetches++
	return c.targets, nil
}

func TestRunnerRefreshesStaleConfigurationForNextRound(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer targetServer.Close()

	makeTargets := func(configID string) protocol.TargetList {
		return protocol.TargetList{
			Version:  protocol.Version,
			ConfigID: configID,
			Config: protocol.MeasurementConfig{
				IntervalMS: 10_000,
				ICMP:       protocol.ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 100},
				HTTP:       protocol.HTTPConfig{Method: "GET", FollowRedirects: true, VerifyTLS: true, TimeoutMS: 1_000},
			},
			Targets: []protocol.Target{{TargetID: "test_http", Address: targetServer.URL, ProbeTypes: []string{"http"}}},
		}
	}
	oldTargets := makeTargets("23ea604f-6e47-5710-bc10-ab9b6a1302a3")
	newTargets := makeTargets("11111111-2222-5333-8444-555555555555")
	client := &refreshingClient{targets: newTargets}
	var logs bytes.Buffer
	r := NewManaged(client, oldTargets, "test", slog.New(slog.NewJSONHandler(&logs, nil)))

	if err := r.runRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.fetches != 1 || r.targets.ConfigID != newTargets.ConfigID {
		t.Fatalf("configuration was not refreshed: fetches=%d config_id=%q", client.fetches, r.targets.ConfigID)
	}
	if err := r.runRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.pushes) != 2 || client.pushes[0].ConfigID != oldTargets.ConfigID || client.pushes[1].ConfigID != newTargets.ConfigID {
		t.Fatalf("unexpected push config IDs: %#v", client.pushes)
	}
	decoder := json.NewDecoder(&logs)
	foundConfigLog := false
	for {
		var entry struct {
			Message string              `json:"msg"`
			Config  protocol.TargetList `json:"config"`
		}
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode log entry: %v", err)
		}
		if entry.Message == "target configuration fetched" {
			foundConfigLog = true
			if entry.Config.ConfigID != newTargets.ConfigID || len(entry.Config.Targets) != len(newTargets.Targets) {
				t.Fatalf("unexpected logged configuration: %#v", entry.Config)
			}
		}
	}
	if !foundConfigLog {
		t.Fatal("fetched configuration was not logged")
	}
}

func TestRunnerRefreshesWhenNoMeasurementsCanBePushed(t *testing.T) {
	oldTargets := protocol.TargetList{
		Version:  protocol.Version,
		ConfigID: "23ea604f-6e47-5710-bc10-ab9b6a1302a3",
		Config: protocol.MeasurementConfig{
			IntervalMS: 10_000,
			ICMP:       protocol.ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 100},
			HTTP:       protocol.HTTPConfig{Method: "GET", TimeoutMS: 1_000},
		},
	}
	newTargets := oldTargets
	newTargets.ConfigID = "11111111-2222-5333-8444-555555555555"
	client := &refreshingClient{targets: newTargets}
	r := NewManaged(client, oldTargets, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := r.runRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.fetches != 1 || r.targets.ConfigID != newTargets.ConfigID {
		t.Fatal("idle configuration was not refreshed")
	}
}
