package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Product struct {
	ID                       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ProductName              string             `bson:"product_name" json:"product_name"`
	ProductDescription       string             `bson:"product_description" json:"product_description"`
	ProductAuthenticationKey string             `bson:"product_authentication_key" json:"product_authentication_key"`
	ProductAuthenticationIV  string             `bson:"product_authentication_iv" json:"product_authentication_iv"`
	ProductCreatedAt         time.Time          `bson:"product_created_at" json:"product_created_at"`
	ProductModifiedAt        time.Time          `bson:"product_modified_at" json:"product_modified_at"`
	ProductCreatedBy         primitive.ObjectID `bson:"product_created_by" json:"product_created_by"`
	ProductModifiedBy        primitive.ObjectID `bson:"product_modified_by" json:"product_modified_by"`
}
