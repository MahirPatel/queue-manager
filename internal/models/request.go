package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	RequestStatusPending    = "pending"
	RequestStatusProcessing = "processing"
	RequestStatusSuccess    = "success"
	RequestStatusFailed     = "failed"
)

type Request struct {
	ID                   primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	ProductID            primitive.ObjectID  `bson:"product_id" json:"product_id"`
	TemplateID           primitive.ObjectID  `bson:"template_id" json:"template_id"`
	DestinationID        primitive.ObjectID  `bson:"destination_id" json:"destination_id"`
	RequestID            string              `bson:"request_id" json:"request_id"`
	TemplateEntryPoint   string              `bson:"template_entry_point" json:"template_entry_point"`
	DestinationEndPoint  string              `bson:"destination_end_point" json:"destination_end_point"`
	RequestHeader        map[string]string   `bson:"request_header" json:"request_header"`
	RequestBody          string              `bson:"request_body" json:"request_body"`
	RequestResponse      string              `bson:"request_response" json:"request_response"`
	RequestStatusCode    int                 `bson:"request_status_code" json:"request_status_code"`
	RequestProcessStatus string              `bson:"request_process_status" json:"request_process_status"`
	RetryCount           int                 `bson:"retry_count" json:"retry_count"`
	RequestAt            time.Time           `bson:"request_at" json:"request_at"`
	RequestExpiryTTL     time.Time           `bson:"request_expiry_ttl" json:"request_expiry_ttl"`
	RecentRequestRetryAt *time.Time          `bson:"recent_request_retry_at,omitempty" json:"recent_request_retry_at,omitempty"`
	RecentRequestRetryBy *primitive.ObjectID `bson:"recent_request_retry_by,omitempty" json:"recent_request_retry_by,omitempty"`
}
