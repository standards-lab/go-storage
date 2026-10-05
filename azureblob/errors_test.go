package azureblob_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/standards-lab/go-storage"
)

// Every method classifies the service's answer the same way; Stat stands in
// for them all. A transport failure, an expired deadline, and the caller's
// cancellation are proved through Probe in client_test.go.
func TestClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
		want   error // the sentinel the error must match, or nil for unclassified
	}{
		{"blob not found", http.StatusNotFound, "BlobNotFound", storage.ErrNotFound},
		{"container not found", http.StatusNotFound, "ContainerNotFound", storage.ErrContainerNotFound},
		{"404 with another code", http.StatusNotFound, "ResourceNotFound", nil},
		{"500", http.StatusInternalServerError, "", storage.ErrUnavailable},
		{"502", http.StatusBadGateway, "", storage.ErrUnavailable},
		{"503 server busy", http.StatusServiceUnavailable, "ServerBusy", storage.ErrUnavailable},
		{"internal error", http.StatusInternalServerError, "InternalError", storage.ErrUnavailable},
		{"operation timed out", http.StatusInternalServerError, "OperationTimedOut", storage.ErrUnavailable},
		{"401", http.StatusUnauthorized, "NoAuthenticationInformation", nil},
		{"403", http.StatusForbidden, "AuthenticationFailed", nil},
		{"400", http.StatusBadRequest, "InvalidInput", nil},
		{"409", http.StatusConflict, "ContainerBeingDeleted", nil},
	}
	sentinels := []error{storage.ErrNotFound, storage.ErrContainerNotFound, storage.ErrUnavailable}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t, failWith(tc.status, tc.code))
			c := newClient(t, testConfig(t, svc.endpoint(), nil))

			_, err := c.Stat(t.Context(), "k")
			if err == nil {
				t.Fatalf("Stat on %d %s = nil, want an error", tc.status, tc.code)
			}
			for _, s := range sentinels {
				if got, want := errors.Is(err, s), s == tc.want; got != want {
					t.Errorf("Stat on %d %s = %v; errors.Is(err, %v) = %t, want %t", tc.status, tc.code, err, s, got, want)
				}
			}
			respErr, ok := errors.AsType[*azcore.ResponseError](err)
			if !ok || respErr.StatusCode != tc.status || respErr.ErrorCode != tc.code {
				t.Errorf("Stat on %d %s = %v, want the SDK's ResponseError still matchable with its status and code", tc.status, tc.code, err)
			}
		})
	}
}
