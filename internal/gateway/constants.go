package gateway

import "time"

const (
	gatewayDialTimeout    = 3 * time.Second
	gatewayTCPKeepAlive   = 30 * time.Second
	gatewayRequestTimeout = 5 * time.Second
)
