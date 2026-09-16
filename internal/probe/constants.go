package probe

import (
	"crypto/tls"
	"time"
)

const (
	httpProbeUserAgent     = "cqu-netprobe"
	minimumProbeTLSVersion = tls.VersionTLS12
	icmpSocketPollInterval = 50 * time.Millisecond
	icmpReceiveBufferBytes = 1500
)
