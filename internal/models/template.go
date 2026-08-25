package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	TemplateStatusActive   = "active"
	TemplateStatusInactive = "inactive"
)

// Destination is one forwarding target under a Transmission Channel. A
// channel with exactly one Destination behaves exactly as it always has
// (every request goes to it, no routing involved). A channel with two or
// more requires TemplateRoutingField to be set on the parent template —
// each Destination is then selected by matching that field's value in the
// decrypted payload against DestinationRoutingValue. ID is stable (never
// reused) so renaming a destination or reordering the list never loses
// track of its backlog — the worker claims pending requests by this ID.
type Destination struct {
	ID                          primitive.ObjectID `bson:"id" json:"id"`
	DestinationName             string             `bson:"destination_name" json:"destination_name"`
	DestinationRoutingValue     string             `bson:"destination_routing_value,omitempty" json:"destination_routing_value,omitempty"`
	DestinationEndPoint         string             `bson:"destination_end_point" json:"destination_end_point"`
	DestinationThrottlingSecond float64            `bson:"destination_throttling_second" json:"destination_throttling_second"`
}

type RequestTemplate struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ProductID           primitive.ObjectID `bson:"product_id" json:"product_id"`
	TemplateName        string             `bson:"template_name" json:"template_name"`
	TemplateDescription string             `bson:"template_description" json:"template_description"`
	TemplateEntryPoint  string             `bson:"template_entry_point" json:"template_entry_point"`
	// TemplateRoutingField names the field in the decrypted JSON payload
	// used to pick a Destination. Empty/unset whenever there's only one
	// Destination — routing only exists once there's something to route
	// between.
	TemplateRoutingField   string             `bson:"template_routing_field,omitempty" json:"template_routing_field,omitempty"`
	Destinations           []Destination      `bson:"destinations" json:"destinations"`
	TemplateRequestTTLDays int                `bson:"template_request_ttl_days" json:"template_request_ttl_days"`
	TemplateStatus         string             `bson:"template_status" json:"template_status"`
	TemplateCreatedAt      time.Time          `bson:"template_created_at" json:"template_created_at"`
	TemplateModifiedAt     time.Time          `bson:"template_modified_at" json:"template_modified_at"`
	TemplateCreatedBy      primitive.ObjectID `bson:"template_created_by" json:"template_created_by"`
	TemplateModifiedBy     primitive.ObjectID `bson:"template_modified_by" json:"template_modified_by"`
}
