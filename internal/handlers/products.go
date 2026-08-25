package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"queue-manager/internal/crypto"
	"queue-manager/internal/db"
	"queue-manager/internal/middleware"
	"queue-manager/internal/models"
)

type productInput struct {
	ProductName              string `json:"product_name"`
	ProductDescription       string `json:"product_description"`
	ProductAuthenticationKey string `json:"product_authentication_key"`
	ProductAuthenticationIV  string `json:"product_authentication_iv"`
}

func (in productInput) validate() string {
	if in.ProductName == "" {
		return "product_name is required"
	}
	if len(in.ProductAuthenticationKey) != crypto.KeySize {
		return "product_authentication_key must be exactly 32 bytes"
	}
	if len(in.ProductAuthenticationIV) != crypto.IVSize {
		return "product_authentication_iv must be exactly 16 bytes"
	}
	return ""
}

func (s *Server) ListProducts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cur, err := s.DB.Collection(db.CollProducts).Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "product_name", Value: 1}}))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cur.Close(ctx)

	products := []models.Product{}
	if err := cur.All(ctx, &products); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (s *Server) GetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var product models.Product
	err = s.DB.Collection(db.CollProducts).FindOne(ctx, bson.M{"_id": id}).Decode(&product)
	if err == mongo.ErrNoDocuments {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (s *Server) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var in productInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	now := time.Now()
	adminID := middleware.AdminIDFromContext(r.Context())
	product := models.Product{
		ID:                       primitive.NewObjectID(),
		ProductName:              in.ProductName,
		ProductDescription:       in.ProductDescription,
		ProductAuthenticationKey: in.ProductAuthenticationKey,
		ProductAuthenticationIV:  in.ProductAuthenticationIV,
		ProductCreatedAt:         now,
		ProductModifiedAt:        now,
		ProductCreatedBy:         adminID,
		ProductModifiedBy:        adminID,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := s.DB.Collection(db.CollProducts).InsertOne(ctx, product); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, "a product with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, product)
}

func (s *Server) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var in productInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{
			"product_name":               in.ProductName,
			"product_description":        in.ProductDescription,
			"product_authentication_key": in.ProductAuthenticationKey,
			"product_authentication_iv":  in.ProductAuthenticationIV,
			"product_modified_at":        time.Now(),
			"product_modified_by":        middleware.AdminIDFromContext(r.Context()),
		},
	}

	res := s.DB.Collection(db.CollProducts).FindOneAndUpdate(
		ctx, bson.M{"_id": id}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)

	var product models.Product
	if err := res.Decode(&product); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		if mongo.IsDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, "a product with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (s *Server) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	count, err := s.DB.Collection(db.CollTemplates).CountDocuments(ctx, bson.M{"product_id": id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "product is referenced by existing templates")
		return
	}

	res, err := s.DB.Collection(db.CollProducts).DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if res.DeletedCount == 0 {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
