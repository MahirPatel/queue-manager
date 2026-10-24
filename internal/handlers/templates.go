package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

type destinationInput struct {
	// ID is empty for a destination being added in this save; set (to an
	// existing destination's id) when editing one in place, so its
	// identity — and any backlog sitting against it — carries over instead
	// of being treated as a brand new destination.
	ID                          string  `json:"id"`
	DestinationName             string  `json:"destination_name"`
	DestinationRoutingValue     string  `json:"destination_routing_value"`
	DestinationEndPoint         string  `json:"destination_end_point"`
	DestinationThrottlingSecond float64 `json:"destination_throttling_second"`
}

type templateInput struct {
	ProductID              string             `json:"product_id"`
	TemplateName           string             `json:"template_name"`
	TemplateDescription    string             `json:"template_description"`
	TemplateEntryPoint     string             `json:"template_entry_point"` // slug, e.g. "customer-sync"
	TemplateRoutingField   string             `json:"template_routing_field"`
	Destinations           []destinationInput `json:"destinations"`
	TemplateRequestTTLDays int                `json:"template_request_ttl_days"`
}

func normalizeEntryPoint(raw string) string {
	slug := strings.Trim(strings.TrimSpace(raw), "/")
	return "/ingest/" + slug
}

// duplicateTemplateMessage turns a unique-index violation into a message
// naming the actual field that collided (template_name vs
// template_entry_point share the same generic duplicate-key error code).
func duplicateTemplateMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "template_name"):
		return "a template with this name already exists"
	case strings.Contains(msg, "template_entry_point"):
		return "a template with this entry point already exists"
	default:
		return "a template with conflicting unique fields already exists"
	}
}

func (in templateInput) validate() (primitive.ObjectID, string) {
	productID, err := primitive.ObjectIDFromHex(in.ProductID)
	if err != nil {
		return primitive.NilObjectID, "product_id is required and must be a valid id"
	}
	if strings.TrimSpace(in.TemplateName) == "" {
		return productID, "template_name is required"
	}
	if strings.Trim(in.TemplateEntryPoint, "/") == "" {
		return productID, "template_entry_point is required"
	}
	if in.TemplateRequestTTLDays <= 0 {
		return productID, "template_request_ttl_days must be greater than 0"
	}

	if len(in.Destinations) == 0 {
		return productID, "at least one destination is required"
	}

	names := map[string]bool{}
	values := map[string]bool{}
	multi := len(in.Destinations) > 1
	if multi && strings.TrimSpace(in.TemplateRoutingField) == "" {
		return productID, "template_routing_field is required when a channel has more than one destination"
	}

	for i, d := range in.Destinations {
		if strings.TrimSpace(d.DestinationName) == "" {
			return productID, fmt.Sprintf("destination %d: destination_name is required", i+1)
		}
		if d.DestinationEndPoint == "" {
			return productID, fmt.Sprintf("destination %d: destination_end_point is required", i+1)
		}
		if d.DestinationThrottlingSecond < 0 {
			return productID, fmt.Sprintf("destination %d: destination_throttling_second cannot be negative", i+1)
		}
		nameKey := strings.ToLower(strings.TrimSpace(d.DestinationName))
		if names[nameKey] {
			return productID, fmt.Sprintf("destination names must be unique within a channel — %q is repeated", d.DestinationName)
		}
		names[nameKey] = true

		if multi {
			if strings.TrimSpace(d.DestinationRoutingValue) == "" {
				return productID, fmt.Sprintf("destination %d: destination_routing_value is required when a channel has more than one destination", i+1)
			}
			valueKey := strings.ToLower(strings.TrimSpace(d.DestinationRoutingValue))
			if values[valueKey] {
				return productID, fmt.Sprintf("destination routing values must be unique within a channel — %q is repeated", d.DestinationRoutingValue)
			}
			values[valueKey] = true
		}
	}

	return productID, ""
}

// buildDestinations turns the input rows into models.Destination values,
// reusing the id (and so any backlog claimed against it) of any existing
// destination the input still references, and minting a fresh id for
// anything new.
func buildDestinations(existing []models.Destination, in []destinationInput) []models.Destination {
	existingByID := make(map[string]bool, len(existing))
	for _, d := range existing {
		existingByID[d.ID.Hex()] = true
	}

	out := make([]models.Destination, 0, len(in))
	multi := len(in) > 1
	for _, di := range in {
		id, err := primitive.ObjectIDFromHex(di.ID)
		if err != nil || !existingByID[di.ID] {
			id = primitive.NewObjectID()
		}

		routingValue := ""
		if multi {
			routingValue = strings.TrimSpace(di.DestinationRoutingValue)
		}

		out = append(out, models.Destination{
			ID:                          id,
			DestinationName:             strings.TrimSpace(di.DestinationName),
			DestinationRoutingValue:     routingValue,
			DestinationEndPoint:         di.DestinationEndPoint,
			DestinationThrottlingSecond: di.DestinationThrottlingSecond,
		})
	}
	return out
}

func (s *Server) ListTemplates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	filter := bson.M{}
	if productIDParam := r.URL.Query().Get("product_id"); productIDParam != "" {
		productID, err := primitive.ObjectIDFromHex(productIDParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid product_id")
			return
		}
		filter["product_id"] = productID
	}

	cur, err := s.DB.Collection(db.CollTemplates).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "template_name", Value: 1}}))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cur.Close(ctx)

	templates := []models.RequestTemplate{}
	if err := cur.All(ctx, &templates); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) GetTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var tmpl models.RequestTemplate
	err = s.DB.Collection(db.CollTemplates).FindOne(ctx, bson.M{"_id": id}).Decode(&tmpl)
	if err == mongo.ErrNoDocuments {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

func (s *Server) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	productID, msg := in.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	count, err := s.DB.Collection(db.CollProducts).CountDocuments(ctx, bson.M{"_id": productID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusBadRequest, "product_id does not reference an existing product")
		return
	}

	entryPoint := normalizeEntryPoint(in.TemplateEntryPoint)
	routingField := ""
	if len(in.Destinations) > 1 {
		routingField = strings.TrimSpace(in.TemplateRoutingField)
	}

	now := time.Now()
	adminID := middleware.AdminIDFromContext(r.Context())
	tmpl := models.RequestTemplate{
		ID:                     primitive.NewObjectID(),
		ProductID:              productID,
		TemplateName:           in.TemplateName,
		TemplateDescription:    in.TemplateDescription,
		TemplateEntryPoint:     entryPoint,
		TemplateRoutingField:   routingField,
		Destinations:           buildDestinations(nil, in.Destinations),
		TemplateRequestTTLDays: in.TemplateRequestTTLDays,
		TemplateStatus:         models.TemplateStatusInactive,
		TemplateCreatedAt:      now,
		TemplateModifiedAt:     now,
		TemplateCreatedBy:      adminID,
		TemplateModifiedBy:     adminID,
	}

	if _, err := s.DB.Collection(db.CollTemplates).InsertOne(ctx, tmpl); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, duplicateTemplateMessage(err))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

func (s *Server) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var in templateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	productID, msg := in.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var existing models.RequestTemplate
	if err := s.DB.Collection(db.CollTemplates).FindOne(ctx, bson.M{"_id": id}).Decode(&existing); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entryPoint := normalizeEntryPoint(in.TemplateEntryPoint)
	routingField := ""
	if len(in.Destinations) > 1 {
		routingField = strings.TrimSpace(in.TemplateRoutingField)
	}
	destinations := buildDestinations(existing.Destinations, in.Destinations)

	update := bson.M{
		"$set": bson.M{
			"product_id":                productID,
			"template_name":             in.TemplateName,
			"template_description":      in.TemplateDescription,
			"template_entry_point":      entryPoint,
			"template_routing_field":    routingField,
			"destinations":              destinations,
			"template_request_ttl_days": in.TemplateRequestTTLDays,
			"template_modified_at":      time.Now(),
			"template_modified_by":      middleware.AdminIDFromContext(r.Context()),
		},
	}

	res := s.DB.Collection(db.CollTemplates).FindOneAndUpdate(
		ctx, bson.M{"_id": id}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)

	var tmpl models.RequestTemplate
	if err := res.Decode(&tmpl); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		if mongo.IsDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, duplicateTemplateMessage(err))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Config changed — restart consumers with the fresh doc if this
	// template is currently active. Stop targets whatever destinations
	// existing had (so one removed from the channel doesn't leak a
	// goroutine); Start then spawns consumers for the current set. Backlog
	// is never at risk here — it's just pending Mongo rows tagged by
	// destination id, and buildDestinations already preserves that id for
	// any destination the edit kept.
	if lifecycle := s.Lifecycle(); lifecycle != nil && existing.TemplateStatus == models.TemplateStatusActive {
		lifecycle.Stop(existing)
		if err := lifecycle.Start(tmpl); err != nil {
			writeError(w, http.StatusInternalServerError, "updated in db but failed to restart consumer: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, tmpl)
}

func (s *Server) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var tmpl models.RequestTemplate
	if err := s.DB.Collection(db.CollTemplates).FindOne(ctx, bson.M{"_id": id}).Decode(&tmpl); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if lifecycle := s.Lifecycle(); lifecycle != nil {
		lifecycle.Stop(tmpl)
	}

	res, err := s.DB.Collection(db.CollTemplates).DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if res.DeletedCount == 0 {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ActivateTemplate(w http.ResponseWriter, r *http.Request) {
	s.setTemplateStatus(w, r, models.TemplateStatusActive)
}

func (s *Server) DeactivateTemplate(w http.ResponseWriter, r *http.Request) {
	s.setTemplateStatus(w, r, models.TemplateStatusInactive)
}

func (s *Server) setTemplateStatus(w http.ResponseWriter, r *http.Request, status string) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{
			"template_status":      status,
			"template_modified_at": time.Now(),
			"template_modified_by": middleware.AdminIDFromContext(r.Context()),
		},
	}
	res := s.DB.Collection(db.CollTemplates).FindOneAndUpdate(
		ctx, bson.M{"_id": id}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)

	var tmpl models.RequestTemplate
	if err := res.Decode(&tmpl); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if lifecycle := s.Lifecycle(); lifecycle != nil {
		if status == models.TemplateStatusActive {
			if err := lifecycle.Start(tmpl); err != nil {
				writeError(w, http.StatusInternalServerError, "activated in db but failed to start consumer: "+err.Error())
				return
			}
		} else {
			// Stop only cancels the consumer goroutines — any backlog sitting
			// in "pending" is untouched and picks up right where it left off
			// the next time this channel is activated.
			lifecycle.Stop(tmpl)
		}
	}

	writeJSON(w, http.StatusOK, tmpl)
}
