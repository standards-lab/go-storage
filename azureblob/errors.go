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

// classify maps an SDK error into the storage sentinels as the package
// documentation lists; nil and an already classified error pass through.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrContainerNotFound) ||
		errors.Is(err, storage.ErrUnavailable) {
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
	case bloberror.HasCode(err, bloberror.ContainerNotFound):
		return fmt.Errorf("%w: %w", storage.ErrContainerNotFound, err)
	case bloberror.HasCode(err, bloberror.BlobNotFound):
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	case respErr.StatusCode >= http.StatusInternalServerError,
		bloberror.HasCode(err, bloberror.ServerBusy, bloberror.OperationTimedOut, bloberror.InternalError):
		return fmt.Errorf("%w: %w", storage.ErrUnavailable, err)
	}
	return err
}

// classifyRead classifies a Get body's read failure. A blob replaced
// before a resumption fails the resumed request's ETag condition, which
// is storage.ErrNotFound because the version being read no longer exists.
// classify handles every other failure.
func classifyRead(err error) error {
	if bloberror.HasCode(err, bloberror.ConditionNotMet) {
		return fmt.Errorf("%w: the blob changed during the read: %w", storage.ErrNotFound, err)
	}
	return classify(err)
}
