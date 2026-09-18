package azureblob

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/standards-lab/go-storage"
)

func responseError(status int, code string) *azcore.ResponseError {
	return &azcore.ResponseError{StatusCode: status, ErrorCode: code}
}

func TestClassify(t *testing.T) {
	transport := &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	alreadyNotFound := fmt.Errorf("%w: %w", storage.ErrNotFound, responseError(http.StatusNotFound, "BlobNotFound"))
	alreadyUnavailable := fmt.Errorf("%w: %w", storage.ErrUnavailable, transport)

	cases := []struct {
		name string
		err  error
		want error // the sentinel the result must match, or nil for unclassified
		same bool  // the result is err itself
	}{
		{"nil", nil, nil, true},
		{"blob not found", responseError(http.StatusNotFound, "BlobNotFound"), storage.ErrNotFound, false},
		{"container not found", responseError(http.StatusNotFound, "ContainerNotFound"), storage.ErrNotFound, false},
		{"404 with another code", responseError(http.StatusNotFound, "ResourceNotFound"), nil, true},
		{"500", responseError(http.StatusInternalServerError, ""), storage.ErrUnavailable, false},
		{"502", responseError(http.StatusBadGateway, ""), storage.ErrUnavailable, false},
		{"503 server busy", responseError(http.StatusServiceUnavailable, "ServerBusy"), storage.ErrUnavailable, false},
		{"internal error", responseError(http.StatusInternalServerError, "InternalError"), storage.ErrUnavailable, false},
		{"operation timed out", responseError(http.StatusInternalServerError, "OperationTimedOut"), storage.ErrUnavailable, false},
		{"401", responseError(http.StatusUnauthorized, "NoAuthenticationInformation"), nil, true},
		{"403", responseError(http.StatusForbidden, "AuthenticationFailed"), nil, true},
		{"400", responseError(http.StatusBadRequest, "InvalidInput"), nil, true},
		{"409", responseError(http.StatusConflict, "ContainerBeingDeleted"), nil, true},
		{"transport failure", transport, storage.ErrUnavailable, false},
		{"deadline exceeded", context.DeadlineExceeded, storage.ErrUnavailable, false},
		{"wrapped deadline exceeded", &url.Error{Op: "Get", Err: context.DeadlineExceeded}, storage.ErrUnavailable, false},
		{"cancelled", context.Canceled, nil, true},
		{"wrapped cancelled", &url.Error{Op: "Get", Err: context.Canceled}, nil, true},
		{"already not found", alreadyNotFound, storage.ErrNotFound, true},
		{"already unavailable", alreadyUnavailable, storage.ErrUnavailable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.err)
			if tc.same && got != tc.err { //nolint:errorlint // identity is the assertion
				t.Fatalf("classify(%v) = %v, want the error itself", tc.err, got)
			}
			if tc.want != nil && !errors.Is(got, tc.want) {
				t.Fatalf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if tc.want == nil && (errors.Is(got, storage.ErrNotFound) || errors.Is(got, storage.ErrUnavailable)) {
				t.Fatalf("classify(%v) = %v, want it unclassified", tc.err, got)
			}
			if tc.err != nil && !errors.Is(got, tc.err) {
				t.Fatalf("classify(%v) = %v, want the cause still matchable", tc.err, got)
			}
		})
	}
}

func TestClassify_KeepsResponseErrorMatchable(t *testing.T) {
	got := classify(responseError(http.StatusServiceUnavailable, "ServerBusy"))
	var respErr *azcore.ResponseError
	if !errors.As(got, &respErr) || respErr.ErrorCode != "ServerBusy" {
		t.Fatalf("classify = %v, want *azcore.ResponseError in the chain", got)
	}
}
