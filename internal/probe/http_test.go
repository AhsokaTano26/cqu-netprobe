package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

func TestHTTPStatusSemantics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(response, request, "/ok", http.StatusFound)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	result, err := HTTP(context.Background(), server.URL+"/redirect", protocol.HTTPConfig{
		Method:          "GET",
		FollowRedirects: false,
		VerifyTLS:       true,
		TimeoutMS:       1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.StatusCode == nil || *result.StatusCode != http.StatusFound || result.DurationMS == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestHTTPRedirectLoopRetainsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 302) }))
	defer server.Close()
	result, err := HTTP(context.Background(), server.URL, protocol.HTTPConfig{Method: "GET", FollowRedirects: true, TimeoutMS: 1000})
	if err == nil || result.StatusCode == nil || *result.StatusCode != 302 || !result.Success {
		t.Fatalf("lost redirect response: %+v, %v", result, err)
	}
}

func TestHTTPBodyTimeoutRetainsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	start := time.Now()
	result, err := HTTP(context.Background(), server.URL, protocol.HTTPConfig{Method: "GET", TimeoutMS: 100})
	if err == nil || result.StatusCode == nil || *result.StatusCode != 200 || !result.Success {
		t.Fatalf("unexpected timeout result: %+v, %v", result, err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("body exceeded request timeout")
	}
}

func TestHTTPVerifyTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	config := protocol.HTTPConfig{Method: "GET", VerifyTLS: true, TimeoutMS: 1000}
	result, err := HTTP(context.Background(), server.URL, config)
	if err == nil || result.Success || result.StatusCode != nil {
		t.Fatal("accepted untrusted certificate")
	}
	config.VerifyTLS = false
	result, err = HTTP(context.Background(), server.URL, config)
	if err != nil || !result.Success {
		t.Fatalf("explicit TLS override failed: %v", err)
	}
}

func TestHTTPTransportFailureHasNullMeasurements(t *testing.T) {
	result, err := HTTP(context.Background(), "://bad-url", protocol.HTTPConfig{
		Method:    "GET",
		TimeoutMS: 100,
	})
	if err == nil {
		t.Fatal("expected request error")
	}
	if result.Success || result.StatusCode != nil || result.DurationMS != nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}
