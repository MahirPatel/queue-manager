package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"queue-manager/internal/crypto"
	"queue-manager/internal/db"
	"queue-manager/internal/models"
)

const maxIngestBodyBytes = 5 << 20 // 5MB

// resolveDestination picks which of the template's destinations a
// decrypted payload should go to. With exactly one destination (or no
// routing field configured — the two always go together, see
// templateInput.validate) it's the only one there is, no payload
// inspection needed at all, preserving the original no-assumptions-about-
// payload-shape behavior. With several, it reads template_routing_field
// out of the payload and matches its value against each destination's
// configured routing value.
func resolveDestination(tmpl models.RequestTemplate, payload []byte) (models.Destination, error) {
	if len(tmpl.Destinations) == 0 {
		return models.Destination{}, fmt.Errorf("template has no destinations configured")
	}
	if len(tmpl.Destinations) == 1 || tmpl.TemplateRoutingField == "" {
		return tmpl.Destinations[0], nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return models.Destination{}, fmt.Errorf("payload must be a JSON object to route by %q", tmpl.TemplateRoutingField)
	}
	raw, ok := parsed[tmpl.TemplateRoutingField]
	if !ok {
		return models.Destination{}, fmt.Errorf("payload is missing routing field %q", tmpl.TemplateRoutingField)
	}
	value := fmt.Sprintf("%v", raw)
	for _, dest := range tmpl.Destinations {
		if dest.DestinationRoutingValue == value {
			return dest, nil
		}
	}
	return models.Destination{}, fmt.Errorf("no destination matches %s=%q", tmpl.TemplateRoutingField, value)
}

// Ingest handles POST /ingest/* — the entry point third parties post
// AES-256-CBC encrypted payloads to. It resolves the template by exact
// path match against template_entry_point, decrypts the body with the
// owning product's key/iv, resolves which destination it routes to,
// persists the request as pending, and wakes that destination's consumer
// so it picks it up right away.
func (s *Server) Ingest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var tmpl models.RequestTemplate
	err := s.DB.Collection(db.CollTemplates).FindOne(ctx, bson.M{
		"template_entry_point": r.URL.Path,
		"template_status":      models.TemplateStatusActive,
	}).Decode(&tmpl)
	if err == mongo.ErrNoDocuments {
		writeError(w, http.StatusNotFound, "unknown or inactive entry point")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var product models.Product
	if err := s.DB.Collection(db.CollProducts).FindOne(ctx, bson.M{"_id": tmpl.ProductID}).Decode(&product); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve product for template")
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxIngestBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	plaintext, err := crypto.Decrypt(string(bodyBytes), []byte(product.ProductAuthenticationKey), []byte(product.ProductAuthenticationIV))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to decrypt payload")
		return
	}
	if !json.Valid(plaintext) {
		writeError(w, http.StatusBadRequest, "decrypted payload is not valid JSON")
		return
	}

	dest, err := resolveDestination(tmpl, plaintext)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	headers := map[string]string{}
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	id := primitive.NewObjectID()
	now := time.Now()
	req := models.Request{
		ID:                   id,
		ProductID:            tmpl.ProductID,
		TemplateID:           tmpl.ID,
		DestinationID:        dest.ID,
		RequestID:            id.Hex(),
		TemplateEntryPoint:   tmpl.TemplateEntryPoint,
		DestinationEndPoint:  dest.DestinationEndPoint,
		RequestHeader:        headers,
		RequestBody:          string(plaintext),
		RequestProcessStatus: models.RequestStatusPending,
		RequestAt:            now,
		RequestExpiryTTL:     now.Add(time.Duration(tmpl.TemplateRequestTTLDays) * 24 * time.Hour),
	}

	if _, err := s.DB.Collection(db.CollRequests).InsertOne(ctx, req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if lifecycle := s.Lifecycle(); lifecycle != nil {
		lifecycle.Wake(dest.ID)
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued", "request_id": req.RequestID})
}
