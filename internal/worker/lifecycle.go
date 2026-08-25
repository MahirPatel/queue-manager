package worker

import (
	"go.mongodb.org/mongo-driver/bson/primitive"

	"queue-manager/internal/models"
)

// Lifecycle is implemented by the consumer Manager. Handlers depend on
// this interface rather than the concrete Manager so it can be swapped
// out (or nil in tests) without handlers needing to know about the
// dispatch mechanism underneath.
type Lifecycle interface {
	// Start spawns a consumer for each of the template's destinations that
	// doesn't already have one running.
	Start(template models.RequestTemplate) error
	// Stop cancels the consumer for every destination on this template
	// (as currently known — pass the pre-edit doc when stopping ahead of
	// an update, so it targets whatever's actually running).
	Stop(template models.RequestTemplate)
	// Wake nudges a destination's consumer to check for new work
	// immediately instead of waiting for its next poll tick. Called right
	// after a request is inserted/reset to pending for that destination.
	Wake(destinationID primitive.ObjectID)
}
