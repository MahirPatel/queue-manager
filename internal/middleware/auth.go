package middleware

import (
	"net/http"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"queue-manager/internal/authtoken"
)

const SessionCookieName = "session"

func requireAuthWithPurposeCheck(jwtSecret string, purposeOK func(string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				unauthorized(w)
				return
			}

			claims, err := authtoken.Parse(jwtSecret, cookie.Value)
			if err != nil || !purposeOK(claims.Purpose) {
				unauthorized(w)
				return
			}

			adminID, err := primitive.ObjectIDFromHex(claims.AdminID)
			if err != nil {
				unauthorized(w)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithAdminID(r.Context(), adminID)))
		})
	}
}

// RequireAuth verifies the session cookie is a valid, non-purpose-limited
// (i.e. full session, not a pending-2fa or 2fa-setup) JWT, and populates
// the admin id into the request context for downstream handlers.
func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return requireAuthWithPurposeCheck(jwtSecret, func(purpose string) bool { return purpose == "" })
}

// RequireAuthAllowMustSetup2FA is RequireAuth plus a PurposeMustSetup2FA
// session — for the small set of routes an admin needs to reach before
// finishing mandatory 2FA enrollment (their own profile, and the 2FA setup
// endpoints). Every other route stays behind plain RequireAuth, which
// rejects a must-setup session, so a must-setup admin genuinely can't touch
// the rest of the app until they enroll.
func RequireAuthAllowMustSetup2FA(jwtSecret string) func(http.Handler) http.Handler {
	return requireAuthWithPurposeCheck(jwtSecret, func(purpose string) bool {
		return purpose == "" || purpose == authtoken.PurposeMustSetup2FA
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
