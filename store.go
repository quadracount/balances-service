package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Store guarda los saldos en balances_db.
//
//	balances:         { _id: group_id, members: { "<user_id>": <saldo neto> } }
//	processed_events: { _id: event_id, processed_at }
type Store struct {
	balances        *mongo.Collection
	processedEvents *mongo.Collection
}

func NewStore(db *mongo.Database) *Store {
	return &Store{
		balances:        db.Collection("balances"),
		processedEvents: db.Collection("processed_events"),
	}
}

// ErrAlreadyProcessed indica que el evento ya se aplicó antes (mensaje repetido).
var ErrAlreadyProcessed = errors.New("evento ya procesado")

// ApplyEvent suma los deltas a los saldos del grupo una sola vez por event_id.
// Primero registra el event_id (índice único por _id): si ya estaba, no hace nada.
// Si después falla el $inc, borra el registro para que el reintento lo vuelva a aplicar.
func (s *Store) ApplyEvent(ctx context.Context, eventID, groupID string, deltas map[string]int64) error {
	_, err := s.processedEvents.InsertOne(ctx, bson.M{"_id": eventID, "processed_at": time.Now().UTC()})
	if mongo.IsDuplicateKeyError(err) {
		return ErrAlreadyProcessed
	}
	if err != nil {
		return fmt.Errorf("registrando evento: %w", err)
	}

	inc := bson.M{}
	for user, delta := range deltas {
		inc["members."+user] = delta
	}
	_, err = s.balances.UpdateOne(ctx,
		bson.M{"_id": groupID},
		bson.M{"$inc": inc},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		if _, delErr := s.processedEvents.DeleteOne(ctx, bson.M{"_id": eventID}); delErr != nil {
			return fmt.Errorf("actualizando saldos: %w (y no se pudo deshacer el registro: %v)", err, delErr)
		}
		return fmt.Errorf("actualizando saldos: %w", err)
	}
	return nil
}

// GroupBalances devuelve el saldo neto de cada miembro que tuvo movimientos en el grupo.
func (s *Store) GroupBalances(ctx context.Context, groupID string) (map[string]int64, error) {
	var doc struct {
		Members map[string]int64 `bson:"members"`
	}
	err := s.balances.FindOne(ctx, bson.M{"_id": groupID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.Members == nil {
		doc.Members = map[string]int64{}
	}
	return doc.Members, nil
}
