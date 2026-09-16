package runner

import (
	"errors"
	"net/http"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/gateway"
)

const (
	pushRetryInitialDelay = time.Second
	pushRetryMaxDelay     = time.Minute
)

type pushErrorKey struct {
	status   int
	code     string
	tooLarge bool
}

type pushBackoff struct {
	key   pushErrorKey
	delay time.Duration
}

func (b *pushBackoff) update(err error) {
	var key pushErrorKey
	var httpErr *gateway.HTTPError
	switch {
	case errors.Is(err, gateway.ErrPushTooLarge):
		key.tooLarge = true
	case errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 600 && httpErr.StatusCode != http.StatusConflict:
		key.status, key.code = httpErr.StatusCode, httpErr.Code
	default:
		*b = pushBackoff{}
		return
	}
	if b.delay == 0 || b.key != key {
		b.delay = pushRetryInitialDelay
	} else {
		b.delay = min(b.delay*2, pushRetryMaxDelay)
	}
	b.key = key
}
