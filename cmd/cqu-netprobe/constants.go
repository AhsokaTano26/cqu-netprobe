package main

import "time"

const (
	targetFetchRetryInitialDelay = time.Second
	targetFetchRetryMultiplier   = 2
	targetFetchRetryMaxDelay     = time.Minute
)
