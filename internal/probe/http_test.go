package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
