package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestFetchTargetsAndPush(t *testing.T) {
	var pushed protocol.PushRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header")
		}
		if request.Header.Get("User-Agent") != "cqu-netprobe/0.1.0" {
			t.Errorf("unexpected user agent %q", request.Header.Get("User-Agent"))
		}
		switch request.URL.Path {
		case "/api/v1/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"version":1,
				"config":{
					"interval_ms":10000,
					"icmp":{"count":5,"interval_ms":200,"timeout_ms":1000},
					"http":{"method":"GET","follow_redirects":true,"verify_tls":true,"timeout_ms":5000},
					"dns":{"transport":"udp","timeout_ms":3000}
				},
				"targets":[{"target_id":"aliyun_dns","address":"223.5.5.5","probe_types":["icmp"]}]
			}`))
		case "/api/v1/push":
			if request.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected content type %q", request.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(request.Body).Decode(&pushed); err != nil {
				t.Errorf("decode push: %v", err)
			}
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	targets, err := client.FetchTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Targets) != 1 || targets.Targets[0].TargetID != "aliyun_dns" {
		t.Fatalf("unexpected targets: %#v", targets.Targets)
	}
	payload := protocol.PushRequest{
		Version:      1,
		Timestamp:    1,
		ProbeVersion: "0.1.0",
		Results: map[string]protocol.TargetMeasurements{
			"aliyun_dns": {ICMP: &protocol.ICMPResult{Sent: 1, LossRatio: 1}},
		},
	}
	if err := client.Push(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if pushed.ProbeVersion != "0.1.0" {
		t.Fatalf("unexpected pushed payload: %#v", pushed)
	}
}

func TestPushReturnsGatewayErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"error":{"code":"rate_limited","message":"rate limited"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token", "test")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Push(context.Background(), protocol.PushRequest{Version: 1, Results: map[string]protocol.TargetMeasurements{}})
	httpErr, ok := err.(*HTTPError)
	if !ok || httpErr.StatusCode != http.StatusTooManyRequests || httpErr.Code != "rate_limited" {
		t.Fatalf("unexpected error: %#v", err)
	}
}
