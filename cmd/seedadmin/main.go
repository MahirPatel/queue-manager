// Command seedadmin creates the very first admin account directly in
// MongoDB. This is the only way to create an admin without already being
// logged in as one — there is deliberately no public self-registration
// endpoint, since anyone able to reach the server would otherwise be able
// to create their own admin account. Every admin after the first is added
// from within the app by an existing admin (see /api/admins).
//
// Idempotent: skips (rather than overwrites) if the email already exists.
//
//	go run ./cmd/seedadmin
//	go run ./cmd/seedadmin -email=someone@example.com -password=... -name="Someone"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"queue-manager/internal/config"
	"queue-manager/internal/db"
	"queue-manager/internal/models"
)

func main() {
	name := flag.String("name", "Admin", "admin name")
	contact := flag.String("contact", "", "admin contact number")
	email := flag.String("email", "admin@example.com", "admin email")
	password := flag.String("password", "12345678", "admin password (min 8 characters)")
	flag.Parse()

	if len(*password) < 8 {
		log.Fatal("password must be at least 8 characters")
	}
	emailNormalized := strings.ToLower(strings.TrimSpace(*email))
	if emailNormalized == "" {
		log.Fatal("email is required")
	}

	cfg := config.Load()

	client, database, err := db.Connect(cfg.MongoURI, cfg.MongoDBName)
	if err != nil {
		log.Fatalf("mongo connect failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := db.EnsureIndexes(ctx, database); err != nil {
		log.Fatalf("ensure indexes failed: %v", err)
	}

	count, err := database.Collection(db.CollAdmins).CountDocuments(ctx, bson.M{"admin_email": emailNormalized})
	if err != nil {
		log.Fatalf("failed to check existing admin: %v", err)
	}
	if count > 0 {
		fmt.Printf("admin %s already exists, skipping\n", emailNormalized)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("failed to hash password: %v", err)
	}

	now := time.Now()
	admin := models.Admin{
		ID:            primitive.NewObjectID(),
		AdminName:     *name,
		AdminContact:  *contact,
		AdminEmail:    emailNormalized,
		AdminPassword: string(hash),
		Enable2FA:     false,
		IsSuperAdmin:  true,
		AdminStatus:   models.AdminStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := database.Collection(db.CollAdmins).InsertOne(ctx, admin); err != nil {
		log.Fatalf("failed to insert admin: %v", err)
	}
	fmt.Printf("created admin %s (id=%s)\n", emailNormalized, admin.ID.Hex())
}
