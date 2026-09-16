//go:build linux && integration

package probe

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRawICMPLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			_, err := pingLinux(context.Background(), net.IPAddr{IP: net.ParseIP(address)}, time.Second, 17, true)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
