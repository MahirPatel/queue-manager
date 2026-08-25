package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	AdminStatusActive   = "active"
	AdminStatusInactive = "inactive"
)

type Admin struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	AdminName      string             `bson:"admin_name" json:"admin_name"`
	AdminContact   string             `bson:"admin_contact" json:"admin_contact"`
	AdminEmail     string             `bson:"admin_email" json:"admin_email"`
	AdminPassword  string             `bson:"admin_password" json:"-"`
	Admin2FASecret string             `bson:"admin_2fa_secret,omitempty" json:"-"`
	Enable2FA      bool               `bson:"enable_2fa" json:"enable_2fa"`
	Require2FA     bool               `bson:"require_2fa" json:"require_2fa"`
	IsSuperAdmin   bool               `bson:"is_super_admin" json:"is_super_admin"`
	AdminStatus    string             `bson:"admin_status" json:"admin_status"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}
