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

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrContainerNotFound) ||
		errors.Is(err, storage.ErrUnavailable) {
		return err
	}

	respErr, ok := errors.AsType[*smithyhttp.ResponseError](err)
	if answered := ok && respErr.HTTPStatusCode() != 0; !answered {
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

func classifyResume(err error) error {
	if respErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok &&
		respErr.HTTPStatusCode() == http.StatusPreconditionFailed && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: the object changed during the read: %w", storage.ErrNotFound, err)
	}
	return classify(err)
}

func errorCode(err error) string {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		return apiErr.ErrorCode()
	}
	return ""
}
