package azureblob

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"

	"github.com/standards-lab/go-storage"
)

// classify maps an SDK error into the storage package's sentinels, wrapped
// in the dual form fmt.Errorf("%w: %w", sentinel, err) so the SDK's error
// stays matchable. Every method returns through it. A nil error stays nil,
// and an error that already matches a sentinel is returned unchanged.
//
// A service response (an *azcore.ResponseError) is classified by its error
// code and status: BlobNotFound and ContainerNotFound match
// storage.ErrNotFound, and a 5xx status or one of the retryable codes
// ServerBusy, OperationTimedOut, and InternalError matches
// storage.ErrUnavailable. Every other response, an authentication or
// authorization failure included, is returned unclassified.
//
// An error with no response is a transport failure: a refused connection, a
// DNS failure, or a deadline the SDK's retry policy consumed. Each matches
// storage.ErrUnavailable. The one exception is context.Canceled, which the
// caller raised on purpose and which says nothing about the store; it passes
// through unclassified. A deadline is classified because the store did not
// answer inside the time the caller allowed, and the wrapped error still
// matches context.DeadlineExceeded for a caller that wants the distinction.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrUnavailable) {
		return err
	}

	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("%w: %w", storage.ErrUnavailable, err)
	}

	switch {
	case bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound):
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	case respErr.StatusCode >= http.StatusInternalServerError,
		bloberror.HasCode(err, bloberror.ServerBusy, bloberror.OperationTimedOut, bloberror.InternalError):
		return fmt.Errorf("%w: %w", storage.ErrUnavailable, err)
	}
	return err
}
