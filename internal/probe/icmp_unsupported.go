//go:build !linux && !windows

package probe

import (
	"context"
	"errors"
	"net"
	"time"
)

func pingOnce(_ context.Context, _ net.IPAddr, _ time.Duration, _ int) (time.Duration, error) {
	return 0, errors.New("ICMP probing is supported only on Linux and Windows")
}
