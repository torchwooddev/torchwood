package databases

import "errors"

// ErrPermissionDenied is returned when the caller lacks document-level permission.
var ErrPermissionDenied = errors.New("permission denied")

// ErrDuplicateKey is returned when a unique constraint violation occurs.
var ErrDuplicateKey = errors.New("duplicate key")

// ErrNoFieldsToUpdate is returned when an update request carries no effective
// field changes (e.g. an increment map containing only zero deltas).
var ErrNoFieldsToUpdate = errors.New("no fields to update")

// ErrDocumentNotFound is returned when an update targets a document that does
// not exist; mapped to codes.NotFound by the app layer.
var ErrDocumentNotFound = errors.New("document not found")

// SimpleDocumentUpdate builds a DocumentUpdate for data and optional permission changes.
func SimpleDocumentUpdate(doc Document, perms []Permission) DocumentUpdate {
	return DocumentUpdate{Document: doc, Permissions: perms}
}
