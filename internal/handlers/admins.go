package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"queue-manager/internal/db"
	"queue-manager/internal/middleware"
	"queue-manager/internal/models"
)

// ListAdmins and CreateAdmin are both behind RequireAuth — there is no
// public admin-registration endpoint. The first admin is created via the
// cmd/seedadmin CLI (marked a super admin); every admin after that is
// added from here by an already-logged-in super admin. Non-super admins
// can still call ListAdmins (view-only) but CreateAdmin/DisableAdmin2FA
// are gated by requireSuperAdmin.

// requireSuperAdmin looks up the calling admin (from the session set by
// RequireAuth) and writes a 403 unless they're a super admin. Checked
// server-side, not just hidden in the UI, since the whole point is that a
// regular admin genuinely can't manage other admins' accounts.
func (s *Server) requireSuperAdmin(r *http.Request, w http.ResponseWriter) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	callerID := middleware.AdminIDFromContext(r.Context())
	var caller models.Admin
	if err := s.DB.Collection(db.CollAdmins).FindOne(ctx, bson.M{"_id": callerID}).Decode(&caller); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return false
	}
	if !caller.IsSuperAdmin {
		writeError(w, http.StatusForbidden, "only a super admin can do this")
		return false
	}
	return true
}

func (s *Server) ListAdmins(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cur, err := s.DB.Collection(db.CollAdmins).Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "admin_name", Value: 1}}))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cur.Close(ctx)

	admins := []models.Admin{}
	if err := cur.All(ctx, &admins); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

func (s *Server) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(r, w) {
		return
	}

	var in struct {
		AdminName     string `json:"admin_name"`
		AdminContact  string `json:"admin_contact"`
		AdminEmail    string `json:"admin_email"`
		AdminPassword string `json:"admin_password"`
		Require2FA    bool   `json:"require_2fa"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.AdminEmail = strings.ToLower(strings.TrimSpace(in.AdminEmail))
	if in.AdminEmail == "" || in.AdminName == "" {
		writeError(w, http.StatusBadRequest, "admin_name and admin_email are required")
		return
	}
	if len(in.AdminPassword) < 8 {
		writeError(w, http.StatusBadRequest, "admin_password must be at least 8 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	admin := models.Admin{
		ID:            primitive.NewObjectID(),
		AdminName:     in.AdminName,
		AdminContact:  in.AdminContact,
		AdminEmail:    in.AdminEmail,
		AdminPassword: string(hash),
		Enable2FA:     false,
		Require2FA:    in.Require2FA,
		AdminStatus:   models.AdminStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := s.DB.Collection(db.CollAdmins).InsertOne(ctx, admin); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, "an admin with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, admin)
}

// DisableAdmin2FA is the recovery path for an admin who's lost their
// authenticator device: it clears the enrolled secret and both the
// enabled and required flags, so the account goes back to plain
// password login. There's no "re-require" action from here — that's a
// deliberate choice by whoever disabled it, not something to auto-reapply.
func (s *Server) DisableAdmin2FA(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(r, w) {
		return
	}

	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	res, err := s.DB.Collection(db.CollAdmins).UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$set":   bson.M{"enable_2fa": false, "require_2fa": false, "updated_at": time.Now()},
			"$unset": bson.M{"admin_2fa_secret": ""},
		},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if res.MatchedCount == 0 {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "2fa_disabled"})
}
