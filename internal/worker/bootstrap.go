package worker

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"queue-manager/internal/db"
	"queue-manager/internal/models"
)

// Bootstrap recovers any request left stuck in "processing" by a previous
// crash (nothing will ever pick those back up otherwise, since consumers
// only poll for "pending"), then starts a consumer for every currently
// active template. Used once at process startup.
func Bootstrap(ctx context.Context, database *mongo.Database, manager *Manager) error {
	if _, err := database.Collection(db.CollRequests).UpdateMany(ctx,
		bson.M{"request_process_status": models.RequestStatusProcessing},
		bson.M{"$set": bson.M{"request_process_status": models.RequestStatusPending}},
	); err != nil {
		return err
	}

	cur, err := database.Collection(db.CollTemplates).Find(ctx, bson.M{"template_status": models.TemplateStatusActive})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	var templates []models.RequestTemplate
	if err := cur.All(ctx, &templates); err != nil {
		return err
	}

	for _, tmpl := range templates {
		if err := manager.Start(tmpl); err != nil {
			slog.Error("worker: failed to start consumer during bootstrap", "template_id", tmpl.ID.Hex(), "err", err)
			continue
		}
	}
	return nil
}
