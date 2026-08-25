package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"queue-manager/internal/db"
	"queue-manager/internal/middleware"
	"queue-manager/internal/models"
)

const (
	defaultRequestsPageSize = 50
	maxRequestsPageSize     = 200
)

// ListRequests backs the admin request grid: filter by template/product/
// status/date range, keyset-paginated on (request_at desc, _id desc).
func (s *Server) ListRequests(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	q := r.URL.Query()
	filter := bson.M{}

	if v := q.Get("template_id"); v != "" {
		id, err := primitive.ObjectIDFromHex(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid template_id")
			return
		}
		filter["template_id"] = id
	}
	if v := q.Get("product_id"); v != "" {
		id, err := primitive.ObjectIDFromHex(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid product_id")
			return
		}
		filter["product_id"] = id
	}
	if v := q.Get("process_status"); v != "" {
		filter["request_process_status"] = v
	}

	dateFilter := bson.M{}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from (expected RFC3339)")
			return
		}
		dateFilter["$gte"] = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to (expected RFC3339)")
			return
		}
		dateFilter["$lte"] = t
	}
	if len(dateFilter) > 0 {
		filter["request_at"] = dateFilter
	}

	if afterAt := q.Get("after_at"); afterAt != "" {
		t, err := time.Parse(time.RFC3339Nano, afterAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid after_at (expected RFC3339)")
			return
		}
		afterID, err := primitive.ObjectIDFromHex(q.Get("after_id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "after_id is required alongside after_at")
			return
		}
		filter["$or"] = []bson.M{
			{"request_at": bson.M{"$lt": t}},
			{"request_at": t, "_id": bson.M{"$lt": afterID}},
		}
	}

	limit := int64(defaultRequestsPageSize)
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 && parsed <= maxRequestsPageSize {
			limit = parsed
		}
	}

	cur, err := s.DB.Collection(db.CollRequests).Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "request_at", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(limit),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cur.Close(ctx)

	requests := []models.Request{}
	if err := cur.All(ctx, &requests); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, requests)
}

func (s *Server) GetRequest(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var req models.Request
	err = s.DB.Collection(db.CollRequests).FindOne(ctx, bson.M{"_id": id}).Decode(&req)
	if err == mongo.ErrNoDocuments {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// RetryRequest is the only retry path in the system — fully manual, admin
// triggered. It resets the request to pending and wakes the destination's
// consumer, the same way ingestion does, so retried requests flow through
// the exact same throttled worker code path.
func (s *Server) RetryRequest(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	adminID := middleware.AdminIDFromContext(r.Context())
	now := time.Now()

	update := bson.M{
		"$set": bson.M{
			"request_process_status":  models.RequestStatusPending,
			"recent_request_retry_at": now,
			"recent_request_retry_by": adminID,
		},
		"$inc": bson.M{"retry_count": 1},
	}

	res := s.DB.Collection(db.CollRequests).FindOneAndUpdate(
		ctx,
		bson.M{"_id": id, "request_process_status": models.RequestStatusFailed},
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)

	var req models.Request
	if err := res.Decode(&req); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusConflict, "request not found or not in a failed state")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var tmpl models.RequestTemplate
	if err := s.DB.Collection(db.CollTemplates).FindOne(ctx, bson.M{"_id": req.TemplateID}).Decode(&tmpl); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve template for retry")
		return
	}
	if tmpl.TemplateStatus != models.TemplateStatusActive {
		writeError(w, http.StatusConflict, "cannot retry: template is not active")
		return
	}

	var dest *models.Destination
	for i := range tmpl.Destinations {
		if tmpl.Destinations[i].ID == req.DestinationID {
			dest = &tmpl.Destinations[i]
			break
		}
	}
	if dest == nil {
		writeError(w, http.StatusConflict, "cannot retry: destination no longer exists on this template")
		return
	}

	// Refresh the denormalized endpoint snapshot to the destination's
	// current value before requeuing — a retry is an explicit admin
	// action, often taken specifically because the endpoint config was
	// just fixed, so it must call the current endpoint, not the one that
	// failed.
	if req.DestinationEndPoint != dest.DestinationEndPoint {
		if _, err := s.DB.Collection(db.CollRequests).UpdateOne(ctx,
			bson.M{"_id": req.ID},
			bson.M{"$set": bson.M{"destination_end_point": dest.DestinationEndPoint}},
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.DestinationEndPoint = dest.DestinationEndPoint
	}

	lifecycle := s.Lifecycle()
	if lifecycle == nil {
		writeError(w, http.StatusInternalServerError, "worker not available")
		return
	}
	lifecycle.Wake(dest.ID)

	writeJSON(w, http.StatusOK, req)
}
