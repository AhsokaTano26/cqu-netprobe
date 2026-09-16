package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/limits"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func HTTP(ctx context.Context, address string, config protocol.HTTPConfig) (protocol.HTTPResult, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Probe the campus connection directly, regardless of process proxy env.
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig = &tls.Config{MinVersion: minimumProbeTLSVersion}
	if !config.VerifyTLS {
		transport.TLSClientConfig.InsecureSkipVerify = true // Configured measurement behavior.
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   protocol.Milliseconds(config.TimeoutMS),
	}
	if !config.FollowRedirects {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	request, err := http.NewRequestWithContext(ctx, config.Method, address, nil)
	if err != nil {
		return protocol.HTTPResult{Success: false}, fmt.Errorf("create HTTP request: %w", err)
	}
	request.Header.Set("User-Agent", httpProbeUserAgent)
	start := time.Now()
	response, err := client.Do(request)
	if response == nil {
		return protocol.HTTPResult{Success: false}, err
	}
	defer response.Body.Close()
	// Include ordinary response transfer time in the measurement while enforcing
	// the client-side body limit against the transparently decompressed stream.
	var read int64
	var readErr error
	if err == nil {
		read, readErr = io.Copy(io.Discard, io.LimitReader(response.Body, limits.MaxHTTPResponseBodyBytes+1))
	}
	duration := float64(time.Since(start)) / float64(time.Millisecond)
	status := response.StatusCode
	result := protocol.HTTPResult{
		Success:    status >= 200 && status < 400,
		StatusCode: &status,
		DurationMS: &duration,
	}
	if err != nil {
		return result, err
	}
	if readErr != nil {
		return result, fmt.Errorf("read HTTP response body: %w", readErr)
	}
	if read > limits.MaxHTTPResponseBodyBytes {
		return result, fmt.Errorf("HTTP response body exceeds the %d MiB client safety limit", limits.MaxHTTPResponseBodyBytes>>20)
	}
	return result, nil
}
