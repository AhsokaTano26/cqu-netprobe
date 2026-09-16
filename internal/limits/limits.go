// Package limits defines client-side safety limits shared across packages.
package limits

const (
	MaxHTTPResponseBodyBytes    = 8 << 20
	MaxTargetConfigBytes        = 1 << 20
	MaxPushBodyBytes            = 64 << 10
	MaxGatewayResponseBodyBytes = 64 << 10
	MaxICMPCount                = 10_000
	MaxProbeVersionBytes        = 32
)
