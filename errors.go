package storage

import "errors"

// Each sentinel classifies one condition. A provider or [Store] wraps the
// sentinel alongside the cause in the dual form
// fmt.Errorf("%w: %w", sentinel, err), so errors.Is matches the class while
// the cause stays recoverable.
var (
	// ErrNotFound reports that no object exists at the key. Get and Stat
	// return it for a missing key. Delete never does: deleting a missing
	// key is a no-op success.
	ErrNotFound = errors.New("storage object not found")

	// ErrTooLarge reports a Put body that exceeds Config.MaxObjectSize.
	// [Store] rejects the body before it reaches the provider.
	ErrTooLarge = errors.New("storage object too large")

	// ErrNotReady reports a call against a [Store] before a successful Start
	// or after Shutdown.
	ErrNotReady = errors.New("storage not ready")

	// ErrUnavailable classifies a connectivity failure: the store is
	// unreachable. A provider returns it wrapped around its SDK's error.
	ErrUnavailable = errors.New("storage unavailable")
)
