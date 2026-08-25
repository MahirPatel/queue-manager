package db

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func EnsureIndexes(ctx context.Context, database *mongo.Database) error {
	admins := database.Collection(CollAdmins)
	if _, err := admins.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "admin_email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}

	products := database.Collection(CollProducts)
	if _, err := products.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "product_name", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}

	templates := database.Collection(CollTemplates)
	templateIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "template_entry_point", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "template_name", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "template_status", Value: 1}}},
		{Keys: bson.D{{Key: "product_id", Value: 1}}},
	}
	if _, err := templates.Indexes().CreateMany(ctx, templateIndexes); err != nil {
		return err
	}

	requests := database.Collection(CollRequests)
	requestIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "request_expiry_ttl", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
		{Keys: bson.D{{Key: "template_id", Value: 1}, {Key: "request_at", Value: -1}}},
		{Keys: bson.D{{Key: "product_id", Value: 1}, {Key: "request_at", Value: -1}}},
		{Keys: bson.D{{Key: "request_process_status", Value: 1}, {Key: "request_at", Value: -1}}},
		{Keys: bson.D{{Key: "request_id", Value: 1}}},
		// Backs the worker's per-destination claim query (find the oldest
		// pending request for one destination) — none of the indexes above
		// cover destination_id.
		{Keys: bson.D{{Key: "destination_id", Value: 1}, {Key: "request_process_status", Value: 1}, {Key: "request_at", Value: 1}}},
	}
	if _, err := requests.Indexes().CreateMany(ctx, requestIndexes); err != nil {
		return err
	}

	return nil
}

// EnsureSuperAdmin is a one-time, idempotent startup fixup: if no admin is
// currently flagged as a super admin (e.g. this install predates the role,
// or seedadmin was run before this field existed), the oldest admin
// account is promoted. Without this, shipping the super-admin-only
// restrictions on CreateAdmin/DisableAdmin2FA into an existing install
// would lock everyone out of admin management with no way back in.
func EnsureSuperAdmin(ctx context.Context, database *mongo.Database) (bool, error) {
	admins := database.Collection(CollAdmins)

	count, err := admins.CountDocuments(ctx, bson.M{"is_super_admin": true})
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	res := admins.FindOneAndUpdate(ctx,
		bson.M{},
		bson.M{"$set": bson.M{"is_super_admin": true}},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "created_at", Value: 1}}),
	)
	if err := res.Err(); err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
