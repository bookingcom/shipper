// Package errors provides detailed error types for Shipper issues.

// Shipper errors can be classified into recoverable and unrecoverable errors.

// A recoverable error represents a transient issue that is expected to
// eventually recover, either on its own (a cluster that was unreachable and
// becomes reachable again) or through use action on resources we cannot
// observe (like an agent acquiring access rights through an external party).
// Actions causing such an error are expected to be retried indefinitely until
// they eventually succeed.

// An unrecoverable error represents an issue that is not expected to ever
// succeed by retrying the actions that caused it without being corrected by
// the user, such as trying to create a malformed or inherently invalid object.
// The actions that cause these errors are NOT expected to be retried, and
// should be permanently dropped from any worker queues.
package errors
