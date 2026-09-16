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
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

const (
	maxTargetResponseSize = 1 << 20
	maxPushBodySize       = 64 << 10
)

type Client struct {
	baseURL    *url.URL
	token      string
	userAgent  string
	httpClient *http.Client
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
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
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	return &Client{
		baseURL:   parsed,
		token:     token,
		userAgent: "cqu-netprobe/" + version,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Second,
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

	limited := io.LimitReader(response.Body, maxTargetResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return protocol.TargetList{}, fmt.Errorf("read target response: %w", err)
	}
	if len(body) > maxTargetResponseSize {
		return protocol.TargetList{}, errors.New("target response exceeds 1 MiB")
	}
	var targets protocol.TargetList
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&targets); err != nil {
		return protocol.TargetList{}, fmt.Errorf("decode target response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return protocol.TargetList{}, fmt.Errorf("decode target response: %w", err)
	}
	if err := targets.Validate(); err != nil {
		return protocol.TargetList{}, fmt.Errorf("validate target response: %w", err)
	}
	return targets, nil
}

func (c *Client) Push(ctx context.Context, payload protocol.PushRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	if len(body) > maxPushBodySize {
		return fmt.Errorf("push request is %d bytes, exceeding the 65536-byte limit", len(body))
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
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	endpoint := *c.baseURL
	endpoint.Path = path
	endpoint.RawPath = ""
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
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	_ = decoder.Decode(&payload)
	return &HTTPError{
		StatusCode: response.StatusCode,
		Code:       payload.Error.Code,
		Message:    payload.Error.Message,
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("trailing JSON data")
}
