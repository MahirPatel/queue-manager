// Package authtoken issues and verifies the JWTs used for admin sessions,
// the short-lived pending-2FA step, and the stateless 2FA-enrollment
// handshake. Shared by internal/handlers (issues tokens) and
// internal/middleware (verifies the session token).
package authtoken

import (
	"github.com/golang-jwt/jwt/v5"
)

// Purpose values distinguish the token kinds sharing this package. Empty
// Purpose means a full session token.
const (
	PurposeTwoFAPending = "2fa-pending"
	PurposeTwoFASetup   = "2fa-setup"
	// PurposeMustSetup2FA marks a session issued to an admin whose account
	// requires 2FA but hasn't enrolled yet. It behaves like a real session
	// cookie but is only accepted by the handful of routes needed to finish
	// enrollment (see middleware.RequireAuthAllowMustSetup2FA) — every other
	// route rejects it via the strict middleware.RequireAuth.
	PurposeMustSetup2FA = "2fa-mustsetup"
)

type Claims struct {
	AdminID string `json:"admin_id"`
	Purpose string `json:"purpose,omitempty"`
	Secret  string `json:"secret,omitempty"` // only set for PurposeTwoFASetup
	jwt.RegisteredClaims
}

func Sign(secret string, claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func Parse(secret, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, jwt.ErrTokenSignatureInvalid
	}
	return claims, nil
}
