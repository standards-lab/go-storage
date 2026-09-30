package storage

import "errors"

// Each sentinel classifies one condition. A provider or [Store] wraps the
// sentinel alongside the cause in the dual form
// fmt.Errorf("%w: %w", sentinel, err), so errors.Is matches the class while
// the cause stays recoverable.
var (
	// ErrNotFound reports that no object exists at the key.
	ErrNotFound = errors.New("storage object not found")

	// ErrContainerNotFound reports that the configured container does not
	// exist. It never matches [ErrNotFound].
	ErrContainerNotFound = errors.New("storage container not found")

	// ErrTooLarge reports a Put whose declared or actual size exceeds
	// Config.MaxObjectSize.
	ErrTooLarge = errors.New("storage object too large")

	// ErrNotReady reports a call against a [Store] before a successful Start
	// or after Shutdown.
	ErrNotReady = errors.New("storage not ready")

	// ErrUnavailable reports a connectivity failure: the store is
	// unreachable.
	ErrUnavailable = errors.New("storage unavailable")
)
