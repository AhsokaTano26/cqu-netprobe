//go:build integration

package probe

import (
	"context"
	"testing"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestICMPLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			result, err := ICMP(context.Background(), address, protocol.ICMPConfig{
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
		})
	}
}
