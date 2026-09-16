package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/AhsokaTano26/cqu-netprobe/internal/limits"
	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

type Client struct {
	baseURL    *url.URL
	token      string
	userAgent  string
	httpClient *http.Client
}

// ErrPushTooLarge identifies a local rejection before any request is sent.
var ErrPushTooLarge = errors.New("push request exceeds the client size limit")

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *HTTPError) Is(target error) bool {
	return target == protocol.ErrConfigStale && e.StatusCode == http.StatusConflict && e.Code == "config_stale"
}

func (e *HTTPError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("gateway returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("gateway returned HTTP %d (%s)", e.StatusCode, e.Code)
}

func NewClient(baseURL, token, version string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse gateway URL: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   gatewayDialTimeout,
		KeepAlive: gatewayTCPKeepAlive,
	}).DialContext
	return &Client{
		baseURL:   parsed,
		token:     token,
		userAgent: "cqu-netprobe/" + version,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   gatewayRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("gateway redirects are not allowed")
			},
		},
	}, nil
}

func (c *Client) FetchTargets(ctx context.Context) (protocol.TargetList, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/api/v1/targets", nil)
	if err != nil {
		return protocol.TargetList{}, err
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return protocol.TargetList{}, fmt.Errorf("fetch targets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return protocol.TargetList{}, decodeHTTPError(response)
	}

	return protocol.DecodeTargets(response.Body)
}

func (c *Client) Push(ctx context.Context, payload protocol.PushRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	if len(body) > limits.MaxPushBodyBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d bytes", ErrPushTooLarge, len(body), limits.MaxPushBodyBytes)
	}
	request, err := c.newRequest(ctx, http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("push measurements: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeHTTPError(response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, limits.MaxGatewayResponseBodyBytes))
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	endpoint := c.baseURL.JoinPath(path)
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create gateway request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("User-Agent", c.userAgent)
	return request, nil
}

func decodeHTTPError(response *http.Response) error {
	var payload protocol.ErrorResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, limits.MaxGatewayResponseBodyBytes))
	_ = decoder.Decode(&payload)
	return &HTTPError{
		StatusCode: response.StatusCode,
		Code:       payload.Error.Code,
		Message:    payload.Error.Message,
	}
}
