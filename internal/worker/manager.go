package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"queue-manager/internal/models"
)

type consumerHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

// Manager owns one goroutine per active destination, each polling Mongo for
// that destination's own pending requests and pacing forwards to its
// endpoint. It implements the Lifecycle interface handlers depend on.
type Manager struct {
	db *mongo.Database

	mu        sync.Mutex
	consumers map[string]*consumerHandle // keyed by destination ID hex
}

func NewManager(database *mongo.Database) *Manager {
	return &Manager{
		db:        database,
		consumers: make(map[string]*consumerHandle),
	}
}

// Start spawns a consumer goroutine for each of the template's destinations
// that doesn't already have one running — a no-op per destination whose
// consumer is already up, so it's safe to call again after e.g. adding a
// destination to an already-active channel.
func (m *Manager) Start(tmpl models.RequestTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, dest := range tmpl.Destinations {
		key := dest.ID.Hex()
		if _, exists := m.consumers[key]; exists {
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		wake := make(chan struct{}, 1)
		m.consumers[key] = &consumerHandle{cancel: cancel, done: done, wake: wake}

		gap := time.Duration(dest.DestinationThrottlingSecond * float64(time.Second))

		go func(destinationID primitive.ObjectID) {
			defer close(done)
			slog.Info("worker: consumer started", "destination_id", destinationID.Hex(), "throttle", gap)
			consumeLoop(ctx, m.db, destinationID, wake, gap)
			slog.Info("worker: consumer stopped", "destination_id", destinationID.Hex())
		}(dest.ID)
	}

	return nil
}

// Stop cancels every one of the template's destination consumers (as
// listed on the template passed in) and waits for each's current
// in-flight request to finish before returning.
func (m *Manager) Stop(tmpl models.RequestTemplate) {
	m.mu.Lock()
	handles := make([]*consumerHandle, 0, len(tmpl.Destinations))
	for _, dest := range tmpl.Destinations {
		key := dest.ID.Hex()
		if h, exists := m.consumers[key]; exists {
			handles = append(handles, h)
			delete(m.consumers, key)
		}
	}
	m.mu.Unlock()

	for _, h := range handles {
		h.cancel()
	}
	for _, h := range handles {
		<-h.done
	}
}

// Wake nudges the given destination's consumer to check for new work right
// away instead of waiting for its next poll tick. A non-blocking send — if
// the consumer is already awake and mid-claim, the pending signal is enough
// to make it check again once it loops, so nothing is lost. A no-op if the
// destination has no running consumer (its channel isn't currently active,
// so nothing should be processing it anyway).
func (m *Manager) Wake(destinationID primitive.ObjectID) {
	m.mu.Lock()
	h, exists := m.consumers[destinationID.Hex()]
	m.mu.Unlock()
	if !exists {
		return
	}
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// StopAll cancels every running consumer and waits for all of them to
// finish their in-flight request. Used on graceful shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	handles := make([]*consumerHandle, 0, len(m.consumers))
	for k, h := range m.consumers {
		handles = append(handles, h)
		delete(m.consumers, k)
	}
	m.mu.Unlock()

	for _, h := range handles {
		h.cancel()
	}
	for _, h := range handles {
		<-h.done
	}
}
