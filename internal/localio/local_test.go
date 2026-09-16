package localio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
	"github.com/AhsokaTano26/cqu-netprobe/internal/runner"
)

func writeInput(t *testing.T, path, address string) {
	t.Helper()
	targets := protocol.TargetList{Version: 1, Config: protocol.MeasurementConfig{
		IntervalMS: 20, ICMP: protocol.ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 1},
		HTTP: protocol.HTTPConfig{Method: "GET", TimeoutMS: 1000},
	}, Targets: []protocol.Target{{TargetID: "test_http", Address: address, ProbeTypes: []string{"http"}}}}
	data, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

type stoppingSink struct {
	*Output
	cancel context.CancelFunc
	count  int
}

func (s *stoppingSink) Push(ctx context.Context, p protocol.PushRequest) error {
	if err := s.Output.Push(ctx, p); err != nil {
		return err
	}
	s.count++
	if s.count == 2 {
		s.cancel()
	}
	return nil
}

func TestLocalRoundsAppendJSONL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	dir := t.TempDir()
	input, outputPath := filepath.Join(dir, "targets.json"), filepath.Join(dir, "results.jsonl")
	writeInput(t, input, server.URL)
	prior := "{\"previous\":true}\n"
	if err := os.WriteFile(outputPath, []byte(prior), 0600); err != nil {
		t.Fatal(err)
	}
	targets, output, err := Open(input, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sink := &stoppingSink{Output: output, cancel: cancel}
	r := runner.New(sink, targets, "local-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.StopOnPushError = true
	if err := r.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected exit: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), prior) {
		t.Fatal("existing output was overwritten")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected original plus two measurements, got %d", len(lines))
	}
	for _, line := range lines[1:] {
		var p protocol.PushRequest
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatal(err)
		}
		m := p.Results["test_http"].HTTP
		if p.Version != 1 || p.Timestamp <= 0 || p.ProbeVersion != "local-test" || m == nil || !m.Success || m.StatusCode == nil || *m.StatusCode != 204 {
			t.Fatalf("invalid payload: %s", line)
		}
	}
}

func TestLocalFileGuards(t *testing.T) {
	dir := t.TempDir()
	input, outputPath := filepath.Join(dir, "input.json"), filepath.Join(dir, "out.jsonl")
	writeInput(t, input, "http://127.0.0.1")
	before, _ := os.ReadFile(input)
	if _, out, err := Open(input, input); err == nil {
		out.Close()
		t.Fatal("accepted same file")
	}
	after, _ := os.ReadFile(input)
	if string(before) != string(after) {
		t.Fatal("input was changed")
	}
	if err := os.WriteFile(outputPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, out, err := Open(input, outputPath); err == nil {
		out.Close()
		t.Fatal("accepted incomplete JSONL line")
	}
	if err := os.WriteFile(input, []byte("{} {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, out, err := Open(input, filepath.Join(dir, "new.jsonl")); err == nil {
		out.Close()
		t.Fatal("accepted invalid input")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid input created output")
	}
}

func TestLocalWriteFailureStopsRunner(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "targets.json")
	writeInput(t, input, "://invalid")
	targets, out, err := Open(input, filepath.Join(dir, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	out.Close()
	r := runner.New(out, targets, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.StopOnPushError = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Run(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write error did not stop runner: %v", err)
	}
}
