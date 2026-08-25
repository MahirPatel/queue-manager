package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"queue-manager/internal/authtoken"
	"queue-manager/internal/db"
	"queue-manager/internal/middleware"
	"queue-manager/internal/models"
)

const pendingCookieName = "pending_2fa"

func setAuthCookie(w http.ResponseWriter, r *http.Request, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, adminID primitive.ObjectID) error {
	claims := authtoken.Claims{
		AdminID: adminID.Hex(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.Config.SessionHours) * time.Hour)),
		},
	}
	token, err := authtoken.Sign(s.Config.JWTSecret, claims)
	if err != nil {
		return err
	}
	setAuthCookie(w, r, middleware.SessionCookieName, token, time.Duration(s.Config.SessionHours)*time.Hour)
	return nil
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AdminEmail    string `json:"admin_email"`
		AdminPassword string `json:"admin_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var admin models.Admin
	err := s.DB.Collection(db.CollAdmins).FindOne(ctx, bson.M{"admin_email": strings.ToLower(strings.TrimSpace(in.AdminEmail))}).Decode(&admin)
	if err != nil || admin.AdminStatus != models.AdminStatusActive {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.AdminPassword), []byte(in.AdminPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if admin.Enable2FA {
		claims := authtoken.Claims{
			AdminID: admin.ID.Hex(),
			Purpose: authtoken.PurposeTwoFAPending,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			},
		}
		token, err := authtoken.Sign(s.Config.JWTSecret, claims)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, pendingCookieName, token, 5*time.Minute)
		writeJSON(w, http.StatusOK, map[string]string{"status": "2fa_required"})
		return
	}

	if admin.Require2FA {
		// Not enrolled yet, but their account mandates it — issue a
		// purpose-limited session that only the 2FA setup routes accept
		// (see middleware.RequireAuthAllowMustSetup2FA) instead of a normal
		// session, so they can't reach the rest of the app until they do.
		claims := authtoken.Claims{
			AdminID: admin.ID.Hex(),
			Purpose: authtoken.PurposeMustSetup2FA,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.Config.SessionHours) * time.Hour)),
			},
		}
		token, err := authtoken.Sign(s.Config.JWTSecret, claims)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, middleware.SessionCookieName, token, time.Duration(s.Config.SessionHours)*time.Hour)
		writeJSON(w, http.StatusOK, map[string]string{"status": "2fa_setup_required"})
		return
	}

	if err := s.issueSession(w, r, admin.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) Verify2FA(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(pendingCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "no pending 2fa session")
		return
	}
	claims, err := authtoken.Parse(s.Config.JWTSecret, cookie.Value)
	if err != nil || claims.Purpose != authtoken.PurposeTwoFAPending {
		writeError(w, http.StatusUnauthorized, "invalid or expired 2fa session")
		return
	}
	adminID, err := primitive.ObjectIDFromHex(claims.AdminID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}

	var in struct {
		TOTPCode string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var admin models.Admin
	if err := s.DB.Collection(db.CollAdmins).FindOne(ctx, bson.M{"_id": adminID}).Decode(&admin); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}

	if !totp.Validate(in.TOTPCode, admin.Admin2FASecret) {
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	clearAuthCookie(w, r, pendingCookieName)
	if err := s.issueSession(w, r, admin.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Setup2FA generates a TOTP secret + QR code but does NOT persist
// anything yet — the secret travels back to ConfirmSetup2FA inside a
// signed, short-lived setup_token so no server-side pending state is
// needed. Requires an existing full session (RequireAuth).
func (s *Server) Setup2FA(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.AdminIDFromContext(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var admin models.Admin
	if err := s.DB.Collection(db.CollAdmins).FindOne(ctx, bson.M{"_id": adminID}).Decode(&admin); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Queue Manager",
		AccountName: admin.AdminEmail,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	claims := authtoken.Claims{
		AdminID: adminID.Hex(),
		Purpose: authtoken.PurposeTwoFASetup,
		Secret:  key.Secret(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}
	setupToken, err := authtoken.Sign(s.Config.JWTSecret, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	img, err := key.Image(200, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      key.Secret(),
		"otpauth_url": key.URL(),
		"qr_data_uri": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		"setup_token": setupToken,
	})
}

func (s *Server) ConfirmSetup2FA(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.AdminIDFromContext(r.Context())

	var in struct {
		SetupToken string `json:"setup_token"`
		TOTPCode   string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	claims, err := authtoken.Parse(s.Config.JWTSecret, in.SetupToken)
	if err != nil || claims.Purpose != authtoken.PurposeTwoFASetup {
		writeError(w, http.StatusUnauthorized, "invalid or expired setup token")
		return
	}
	if claims.AdminID != adminID.Hex() {
		writeError(w, http.StatusForbidden, "setup token does not belong to this session")
		return
	}
	if !totp.Validate(in.TOTPCode, claims.Secret) {
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := s.DB.Collection(db.CollAdmins).UpdateOne(ctx,
		bson.M{"_id": adminID},
		bson.M{"$set": bson.M{"admin_2fa_secret": claims.Secret, "enable_2fa": true, "updated_at": time.Now()}},
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If this confirm came from the mandatory-setup flow, the current
	// cookie is still a purpose-limited must-setup session — upgrade it to
	// a full session now that enrollment is done. Harmless no-op refresh
	// for the normal (already-fully-logged-in) self-service path.
	if err := s.issueSession(w, r, adminID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "2fa_enabled"})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookie(w, r, middleware.SessionCookieName)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Me returns the authenticated admin's own profile — used by the frontend
// both as a lightweight "am I logged in" check and to show who's logged in.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.AdminIDFromContext(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var admin models.Admin
	if err := s.DB.Collection(db.CollAdmins).FindOne(ctx, bson.M{"_id": adminID}).Decode(&admin); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	writeJSON(w, http.StatusOK, admin)
}
