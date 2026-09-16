//go:build integration

package probe

import (
	"context"
	"testing"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestICMPLoopback(t *testing.T) {
	result, err := ICMP(context.Background(), "127.0.0.1", protocol.ICMPConfig{
		Count:      2,
		IntervalMS: 10,
		TimeoutMS:  1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Received != 2 || result.MinRTTMS == nil || result.JitterMS == nil {
		t.Fatalf("unexpected loopback result: %#v", result)
	}
}
