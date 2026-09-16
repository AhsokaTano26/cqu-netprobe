package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

type pingOutcome struct {
	sequence int
	rtt      time.Duration
	err      error
}

func ICMP(ctx context.Context, address string, config protocol.ICMPConfig) (protocol.ICMPResult, error) {
	result := protocol.ICMPResult{
		Sent:      config.Count,
		LossRatio: 1,
	}
	resolveContext, cancel := context.WithTimeout(ctx, protocol.Milliseconds(config.TimeoutMS))
	addresses, err := net.DefaultResolver.LookupIPAddr(resolveContext, address)
	cancel()
	if err != nil {
		return result, fmt.Errorf("resolve ICMP target: %w", err)
	}
	if len(addresses) == 0 {
		return result, errors.New("resolve ICMP target: no addresses returned")
	}
	target := preferIPv4(addresses)

	outcomes := make(chan pingOutcome, config.Count)
	var group sync.WaitGroup
	interval := protocol.Milliseconds(config.IntervalMS)
	timeout := protocol.Milliseconds(config.TimeoutMS)
	for sequence := 0; sequence < config.Count; sequence++ {
		group.Add(1)
		go func(sequence int) {
			defer group.Done()
			if sequence > 0 {
				timer := time.NewTimer(time.Duration(sequence) * interval)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					outcomes <- pingOutcome{sequence: sequence, err: ctx.Err()}
					return
				case <-timer.C:
				}
			}
			rtt, err := pingOnce(ctx, target, timeout, sequence)
			outcomes <- pingOutcome{sequence: sequence, rtt: rtt, err: err}
		}(sequence)
	}
	group.Wait()
	close(outcomes)

	successful := make([]pingOutcome, 0, config.Count)
	var joined error
	for outcome := range outcomes {
		if outcome.err != nil {
			joined = errors.Join(joined, outcome.err)
			continue
		}
		successful = append(successful, outcome)
	}
	sort.Slice(successful, func(i, j int) bool { return successful[i].sequence < successful[j].sequence })
	result.Received = len(successful)
	result.Success = result.Received > 0
	result.LossRatio = float64(result.Sent-result.Received) / float64(result.Sent)
	if len(successful) == 0 {
		return result, joined
	}

	minimum, maximum, total := successful[0].rtt, successful[0].rtt, time.Duration(0)
	for _, outcome := range successful {
		minimum = min(minimum, outcome.rtt)
		maximum = max(maximum, outcome.rtt)
		total += outcome.rtt
	}
	minMS := durationMilliseconds(minimum)
	avgMS := durationMilliseconds(total) / float64(len(successful))
	maxMS := durationMilliseconds(maximum)
	result.MinRTTMS = &minMS
	result.AvgRTTMS = &avgMS
	result.MaxRTTMS = &maxMS
	if len(successful) > 1 {
		var differenceTotal time.Duration
		for i := 1; i < len(successful); i++ {
			difference := successful[i].rtt - successful[i-1].rtt
			if difference < 0 {
				difference = -difference
			}
			differenceTotal += difference
		}
		jitterMS := durationMilliseconds(differenceTotal) / float64(len(successful)-1)
		result.JitterMS = &jitterMS
	}
	return result, joined
}

func preferIPv4(addresses []net.IPAddr) net.IPAddr {
	for _, address := range addresses {
		if address.IP.To4() != nil {
			return address
		}
	}
	return addresses[0]
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
