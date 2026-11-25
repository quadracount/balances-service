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
//	payments:         { _id, group_id, from_user_id, to_user_id, amount, created_at }
type Store struct {
	balances        *mongo.Collection
	processedEvents *mongo.Collection
	payments        *mongo.Collection
}

func NewStore(db *mongo.Database) *Store {
	return &Store{
		balances:        db.Collection("balances"),
		processedEvents: db.Collection("processed_events"),
		payments:        db.Collection("payments"),
	}
}

// Payment es un pago registrado entre dos miembros de un grupo.
type Payment struct {
	ID         string    `bson:"_id"`
	GroupID    string    `bson:"group_id"`
	FromUserID string    `bson:"from_user_id"`
	ToUserID   string    `bson:"to_user_id"`
	Amount     int64     `bson:"amount"`
	CreatedAt  time.Time `bson:"created_at"`
}

// RecordPayment guarda el pago y lo aplica a los saldos (una sola vez, usando el
// id del pago como event_id). Si no se pueden actualizar los saldos, borra el pago.
func (s *Store) RecordPayment(ctx context.Context, p Payment, deltas map[string]int64) error {
	if _, err := s.payments.InsertOne(ctx, p); err != nil {
		return fmt.Errorf("guardando pago: %w", err)
	}
	if err := s.ApplyEvent(ctx, p.ID, p.GroupID, deltas); err != nil {
		if _, delErr := s.payments.DeleteOne(ctx, bson.M{"_id": p.ID}); delErr != nil {
			return fmt.Errorf("%w (y no se pudo borrar el pago: %v)", err, delErr)
		}
		return err
	}
	return nil
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
