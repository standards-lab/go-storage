package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

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

	// The SDK wraps a send failure in a ResponseError too, one with no
	// response and status 0, so an answer is a ResponseError with a status.
	respErr, ok := errors.AsType[*smithyhttp.ResponseError](err)
	if !ok || respErr.HTTPStatusCode() == 0 {
		// No answer arrived: the endpoint was unreachable, the deadline
		// passed, or the caller cancelled. Only the last passes through.
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("%w: %w", storage.ErrUnavailable, err)
	}

	switch code := errorCode(err); {
	case code == "NoSuchBucket":
		return fmt.Errorf("%w: %w", storage.ErrContainerNotFound, err)
	case code == "NoSuchKey":
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	case respErr.HTTPStatusCode() >= http.StatusInternalServerError,
		code == "SlowDown", code == "ServiceUnavailable", code == "InternalError":
		return fmt.Errorf("%w: %w", storage.ErrUnavailable, err)
	}
	return err
}

// classifyRead classifies a Get body's read failure. An object replaced
// before a resumption fails the resumed request's If-Match condition, which
// is storage.ErrNotFound because the version being read no longer exists,
// as is one deleted, whose NoSuchKey classify maps. classify handles every
// other failure.
func classifyRead(err error) error {
	if respErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok &&
		respErr.HTTPStatusCode() == http.StatusPreconditionFailed && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: the object changed during the read: %w", storage.ErrNotFound, err)
	}
	return classify(err)
}

// errorCode returns the S3 error code err carries, or "" when it carries
// none.
func errorCode(err error) string {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		return apiErr.ErrorCode()
	}
	return ""
}
