package worker

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"queue-manager/internal/db"
	"queue-manager/internal/httpclient"
	"queue-manager/internal/models"
)

// pollFallbackInterval bounds how long a consumer can go without checking
// its destination for new work when it never receives a wake signal — e.g.
// the narrow window between a template flipping to active in Mongo and its
// consumer goroutine finishing registration. The wake channel makes this
// the worst case, not the common one.
const pollFallbackInterval = 300 * time.Millisecond

// consumeLoop repeatedly claims and forwards the oldest pending request for
// one destination, pacing the gap between forwards per that destination's
// throttling_second. When there's nothing to claim, it blocks until woken
// (a new/retried request for this destination), the poll tick fires, or
// ctx is cancelled.
func consumeLoop(ctx context.Context, database *mongo.Database, destinationID primitive.ObjectID, wake <-chan struct{}, gap time.Duration) {
	ticker := time.NewTicker(pollFallbackInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		claimed := claimAndProcessNext(database, destinationID)
		if !claimed {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-ticker.C:
			}
			continue
		}

		if gap <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(gap):
		}
	}
}

// claimAndProcessNext atomically claims the oldest pending request for
// destinationID (if any), forwards it, and records the outcome. Returns
// false if there was nothing to claim. Uses its own background timeouts
// rather than the consumer's cancellable context, deliberately — once a
// request is claimed it should run to completion (claim -> forward ->
// write outcome) even if Stop is called mid-flight; consumeLoop's caller
// already waits on the consumer's done channel for exactly that reason.
func claimAndProcessNext(database *mongo.Database, destinationID primitive.ObjectID) bool {
	claimCtx, claimCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer claimCancel()

	// Guarded pending->processing transition: this atomic FindOneAndUpdate
	// is the only synchronization primitive this needs — it's what makes
	// it safe for more than one consumer to poll the same destination
	// (e.g. across a future multi-instance deployment) without double
	// delivery.
	res := database.Collection(db.CollRequests).FindOneAndUpdate(
		claimCtx,
		bson.M{"destination_id": destinationID, "request_process_status": models.RequestStatusPending},
		bson.M{"$set": bson.M{"request_process_status": models.RequestStatusProcessing}},
		options.FindOneAndUpdate().
			SetSort(bson.D{{Key: "request_at", Value: 1}}).
			SetReturnDocument(options.After),
	)

	var req models.Request
	if err := res.Decode(&req); err != nil {
		if err != mongo.ErrNoDocuments {
			slog.Error("worker: failed to claim next request", "destination_id", destinationID.Hex(), "err", err)
		}
		return false
	}

	statusCode, respBody, forwardErr := httpclient.Forward(req.DestinationEndPoint, []byte(req.RequestBody), "application/json")

	processStatus := models.RequestStatusFailed
	responseText := string(respBody)
	if forwardErr != nil {
		responseText = forwardErr.Error()
	} else if statusCode >= 200 && statusCode < 300 {
		processStatus = models.RequestStatusSuccess
	}

	update := bson.M{
		"request_status_code":    statusCode,
		"request_response":       responseText,
		"request_process_status": processStatus,
	}

	updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer updateCancel()
	if _, err := database.Collection(db.CollRequests).UpdateOne(updateCtx, bson.M{"_id": req.ID}, bson.M{"$set": update}); err != nil {
		slog.Error("worker: failed to write back request outcome", "id", req.ID.Hex(), "err", err)
	}

	return true
}
