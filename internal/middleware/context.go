package middleware

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ctxKey string

const adminIDKey ctxKey = "admin_id"

func WithAdminID(ctx context.Context, id primitive.ObjectID) context.Context {
	return context.WithValue(ctx, adminIDKey, id)
}

// AdminIDFromContext returns the authenticated admin's id, or the zero
// ObjectID if RequireAuth hasn't populated the context (routes not yet
// behind auth).
func AdminIDFromContext(ctx context.Context) primitive.ObjectID {
	if id, ok := ctx.Value(adminIDKey).(primitive.ObjectID); ok {
		return id
	}
	return primitive.NilObjectID
}
