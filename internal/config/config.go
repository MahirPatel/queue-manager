package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         string
	MongoURI     string
	MongoDBName  string
	JWTSecret    string
	SessionHours int
	WebDir       string
}

func Load() Config {
	_ = godotenv.Load()

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		jwtSecret = randomHex(32)
		slog.Warn("JWT_SECRET not set — generated an ephemeral one; every admin session will be invalidated on restart. Set JWT_SECRET in .env for a stable secret.")
	}

	return Config{
		Port:         getEnv("PORT", "8080"),
		MongoURI:     getEnv("MONGO_URI", "mongodb://127.0.0.1:27017"),
		MongoDBName:  getEnv("MONGO_DB_NAME", "queue_manager"),
		JWTSecret:    jwtSecret,
		SessionHours: 12,
		WebDir:       getEnv("WEB_DIR", "web"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("config: failed to generate random JWT secret: " + err.Error())
	}
	return hex.EncodeToString(b)
}
