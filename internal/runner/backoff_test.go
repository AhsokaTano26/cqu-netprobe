package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/gateway"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestPushBackoffClassificationAndReset(t *testing.T) {
	var b pushBackoff
	for status := 400; status < 600; status++ {
		b = pushBackoff{}
		b.update(&gateway.HTTPError{StatusCode: status})
		want := time.Second
		if status == 409 {
			want = 0
		}
		if b.delay != want {
			t.Fatalf("status %d: delay %v, want %v", status, b.delay, want)
		}
	}
	err500 := &gateway.HTTPError{StatusCode: 500, Code: "internal_error"}
	b = pushBackoff{}
	for _, want := range []time.Duration{1, 2, 4, 8, 16, 32, 60, 60} {
		b.update(fmt.Errorf("wrapped: %w", err500))
		if b.delay != want*time.Second {
			t.Fatalf("delay %v, want %vs", b.delay, want)
		}
	}
	b.update(&gateway.HTTPError{StatusCode: 500, Code: "internal_error", Message: "different request ID"})
	if b.delay != time.Minute {
		t.Fatal("message change reset backoff")
	}
	for _, err := range []error{
		&gateway.HTTPError{StatusCode: 500, Code: "different_code"},
		&gateway.HTTPError{StatusCode: 503, Code: "different_code"},
		fmt.Errorf("size: %w", gateway.ErrPushTooLarge),
	} {
		b.update(err)
		if b.delay != time.Second {
			t.Fatalf("changed error did not restart backoff: %v", err)
		}
	}
	for _, err := range []error{nil, errors.New("network failure"), context.Canceled, protocol.ErrConfigStale,
		&gateway.HTTPError{StatusCode: 409, Code: "config_stale"},
		&gateway.HTTPError{StatusCode: 409, Code: "other_conflict"},
		&gateway.HTTPError{StatusCode: 302}, &gateway.HTTPError{StatusCode: 600},
	} {
		b.update(err500)
		b.update(err500)
		b.update(err)
		if b.delay != 0 {
			t.Fatalf("error %v did not clear backoff", err)
		}
		b.update(err500)
		if b.delay != time.Second {
			t.Fatal("error sequence was not reset")
		}
	}
}

type backoffClient struct {
	targets  protocol.TargetList
	push     func(context.Context, protocol.PushRequest) error
	fetches  int
	fetchErr error
}

func (c *backoffClient) Push(ctx context.Context, p protocol.PushRequest) error {
	return c.push(ctx, p)
}
func (c *backoffClient) FetchTargets(context.Context) (protocol.TargetList, error) {
	c.fetches++
	return c.targets, c.fetchErr
}

func backoffTargets() protocol.TargetList {
	return protocol.TargetList{Version: 1, ConfigID: "23ea604f-6e47-5710-bc10-ab9b6a1302a3",
		Config: protocol.MeasurementConfig{IntervalMS: 20,
			ICMP: protocol.ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 1},
			HTTP: protocol.HTTPConfig{Method: "GET", TimeoutMS: 10}},
		Targets: []protocol.Target{{TargetID: "test_http", Address: "://invalid", ProbeTypes: []string{"http"}}}}
}

func TestRunnerBackoffScheduleAndRecovery(t *testing.T) {
	for _, refreshFails := range []bool{false, true} {
		t.Run(fmt.Sprint(refreshFails), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				client := &backoffClient{targets: backoffTargets()}
				if refreshFails {
					client.fetchErr = errors.New("fetch failed")
				}
				var times []time.Time
				client.push = func(_ context.Context, p protocol.PushRequest) error {
					times = append(times, time.Now())
					if p.Timestamp != time.Now().Unix() {
						t.Fatal("retried historical payload")
					}
					if len(times) == 10 {
						cancel()
						return nil
					}
					if len(times) == 9 {
						return nil
					}
					return &gateway.HTTPError{StatusCode: 500, Code: "internal_error"}
				}
				r := NewManaged(client, client.targets, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err := r.Run(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("unexpected exit: %v", err)
				}
				want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute, 20 * time.Millisecond}
				for i, delay := range want {
					if got := times[i+1].Sub(times[i]); got != delay {
						t.Fatalf("round %d: got %v, want %v", i, got, delay)
					}
				}
				if client.fetches != 8 {
					t.Fatalf("fetches=%d, want 8", client.fetches)
				}
			})
		})
	}
}

func TestRunnerBackoffCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &backoffClient{targets: backoffTargets()}
		pushes := 0
		client.push = func(context.Context, protocol.PushRequest) error {
			pushes++
			time.AfterFunc(100*time.Millisecond, cancel)
			return &gateway.HTTPError{StatusCode: 400}
		}
		start := time.Now()
		r := NewManaged(client, client.targets, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := r.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected exit: %v", err)
		}
		if time.Since(start) != 100*time.Millisecond || pushes != 1 || client.fetches != 0 {
			t.Fatal("backoff did not cancel promptly")
		}
	})
}

func TestRunnerPreservesNormalScheduleAnd409Handling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		fetches int
	}{
		{"503", &gateway.HTTPError{StatusCode: 503}, 1},
		{"stale", &gateway.HTTPError{StatusCode: 409, Code: "config_stale"}, 1},
		{"other conflict", &gateway.HTTPError{StatusCode: 409, Code: "other"}, 0},
		{"network", errors.New("connection failure"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				client := &backoffClient{targets: backoffTargets()}
				client.targets.Config.IntervalMS = 10000
				pushes := 0
				client.push = func(context.Context, protocol.PushRequest) error {
					pushes++
					if pushes == 2 {
						cancel()
						return nil
					}
					return tc.err
				}
				r := NewManaged(client, client.targets, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
				start := time.Now()
				if err := r.Run(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("unexpected exit: %v", err)
				}
				if time.Since(start) != 10*time.Second || client.fetches != tc.fetches {
					t.Fatalf("elapsed=%v fetches=%d, want 10s and %d", time.Since(start), client.fetches, tc.fetches)
				}
			})
		})
	}
}

func TestRunnerRecoversFromOversizedPush(t *testing.T) {
	old := backoffTargets()
	old.Targets = nil
	for i := 0; i < 1000; i++ {
		old.Targets = append(old.Targets, protocol.Target{TargetID: fmt.Sprintf("target_%04d", i), Address: "://invalid", ProbeTypes: []string{"http"}})
	}
	updated := backoffTargets()
	updated.ConfigID = "11111111-2222-5333-8444-555555555555"
	requests := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests <- req.URL.Path
		if req.URL.Path == "/api/v1/targets" {
			_ = json.NewEncoder(w).Encode(updated)
			return
		}
		var p protocol.PushRequest
		if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		if p.ConfigID != updated.ConfigID || len(p.Results) != 1 {
			t.Errorf("unexpected refreshed payload: %+v", p)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := gateway.NewClient(server.URL, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	r := NewManaged(client, old, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := r.runRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.pushBackoff.delay != time.Second || len(requests) != 0 {
		t.Fatal("oversized push did not trigger local backoff")
	}
	if err := r.runRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.pushBackoff.delay != 0 || r.targets.ConfigID != updated.ConfigID {
		t.Fatal("runner did not recover")
	}
	if len(requests) != 2 {
		t.Fatalf("requests=%d, want refresh then push", len(requests))
	}
	if <-requests != "/api/v1/targets" || <-requests != "/api/v1/push" {
		t.Fatal("push happened before refresh")
	}
}
