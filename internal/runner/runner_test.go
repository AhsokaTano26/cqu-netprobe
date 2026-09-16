package runner

import (
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
		Version: protocol.Version,
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
