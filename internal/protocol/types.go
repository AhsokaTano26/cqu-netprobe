package protocol

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/limits"
)

const Version = 1

var (
	targetIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	configIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

var ErrConfigStale = errors.New("gateway measurement configuration is stale")

type TargetList struct {
	Version  int               `json:"version"`
	Config   MeasurementConfig `json:"config"`
	ConfigID string            `json:"config_id"`
	Targets  []Target          `json:"targets"`
}

type MeasurementConfig struct {
	IntervalMS int64      `json:"interval_ms"`
	ICMP       ICMPConfig `json:"icmp"`
	HTTP       HTTPConfig `json:"http"`
}

type ICMPConfig struct {
	Count      int   `json:"count"`
	IntervalMS int64 `json:"interval_ms"`
	TimeoutMS  int64 `json:"timeout_ms"`
}

type HTTPConfig struct {
	Method          string `json:"method"`
	FollowRedirects bool   `json:"follow_redirects"`
	VerifyTLS       bool   `json:"verify_tls"`
	TimeoutMS       int64  `json:"timeout_ms"`
}

type Target struct {
	TargetID   string   `json:"target_id"`
	Address    string   `json:"address"`
	ProbeTypes []string `json:"probe_types"`
}

type PushRequest struct {
	Version      int                           `json:"version"`
	Timestamp    int64                         `json:"timestamp"`
	ProbeVersion string                        `json:"probe_version"`
	ConfigID     string                        `json:"config_id"`
	Results      map[string]TargetMeasurements `json:"results"`
}

type TargetMeasurements struct {
	ICMP *ICMPResult `json:"icmp,omitempty"`
	HTTP *HTTPResult `json:"http,omitempty"`
}

type ICMPResult struct {
	Success   bool     `json:"success"`
	Sent      int      `json:"sent"`
	Received  int      `json:"received"`
	LossRatio float64  `json:"loss_ratio"`
	MinRTTMS  *float64 `json:"min_rtt_ms"`
	AvgRTTMS  *float64 `json:"avg_rtt_ms"`
	MaxRTTMS  *float64 `json:"max_rtt_ms"`
	JitterMS  *float64 `json:"jitter_ms"`
}

type HTTPResult struct {
	Success    bool     `json:"success"`
	StatusCode *int     `json:"status_code"`
	DurationMS *float64 `json:"duration_ms"`
}

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (l TargetList) Validate() error {
	if l.Version != Version {
		return fmt.Errorf("unsupported target-list version %d", l.Version)
	}
	if !configIDPattern.MatchString(l.ConfigID) {
		return errors.New("config_id must be a lowercase UUID v5")
	}
	if err := validateDurationMS("config.interval_ms", l.Config.IntervalMS); err != nil {
		return err
	}
	if l.Config.ICMP.Count < 1 {
		return errors.New("config.icmp.count must be positive")
	}
	// A defensive ceiling prevents a malformed Gateway response from causing
	// an unbounded allocation while remaining far above a useful probe count.
	if l.Config.ICMP.Count > limits.MaxICMPCount {
		return fmt.Errorf("config.icmp.count exceeds the client safety limit of %d", limits.MaxICMPCount)
	}
	if err := validateDurationMS("config.icmp.interval_ms", l.Config.ICMP.IntervalMS); err != nil {
		return err
	}
	if err := validateDurationMS("config.icmp.timeout_ms", l.Config.ICMP.TimeoutMS); err != nil {
		return err
	}
	worstICMP, ok := safeICMPRoundDuration(l.Config.ICMP)
	if !ok {
		return errors.New("config.icmp duration overflows")
	}
	if worstICMP >= l.Config.IntervalMS {
		return errors.New("config.icmp worst-case duration must be less than config.interval_ms")
	}
	if l.Config.HTTP.Method != "GET" {
		return fmt.Errorf("config.http.method must be GET, got %q", l.Config.HTTP.Method)
	}
	if err := validateDurationMS("config.http.timeout_ms", l.Config.HTTP.TimeoutMS); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(l.Targets))
	for i, target := range l.Targets {
		if !targetIDPattern.MatchString(target.TargetID) {
			return fmt.Errorf("targets[%d].target_id is invalid", i)
		}
		if _, exists := seen[target.TargetID]; exists {
			return fmt.Errorf("duplicate target_id %q", target.TargetID)
		}
		seen[target.TargetID] = struct{}{}
		if strings.TrimSpace(target.Address) == "" {
			return fmt.Errorf("target %q has an empty address", target.TargetID)
		}
		if len(target.ProbeTypes) == 0 {
			return fmt.Errorf("target %q has no probe types", target.TargetID)
		}
		types := make(map[string]struct{}, len(target.ProbeTypes))
		for _, probeType := range target.ProbeTypes {
			switch probeType {
			case "icmp", "http", "dns":
			default:
				return fmt.Errorf("target %q has unknown probe type %q", target.TargetID, probeType)
			}
			if _, exists := types[probeType]; exists {
				return fmt.Errorf("target %q repeats probe type %q", target.TargetID, probeType)
			}
			types[probeType] = struct{}{}
		}
	}
	return nil
}

func validateDurationMS(name string, value int64) error {
	if value < 1 {
		return fmt.Errorf("%s must be positive", name)
	}
	if value > int64(math.MaxInt64/time.Millisecond) {
		return fmt.Errorf("%s is too large", name)
	}
	return nil
}

func safeICMPRoundDuration(config ICMPConfig) (int64, bool) {
	intervals := int64(config.Count - 1)
	if intervals > (math.MaxInt64-config.TimeoutMS)/config.IntervalMS {
		return 0, false
	}
	return intervals*config.IntervalMS + config.TimeoutMS, true
}

func Milliseconds(value int64) time.Duration {
	return time.Duration(value) * time.Millisecond
}
