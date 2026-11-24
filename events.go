package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/quadracount/balances-service/ledger"
)

const (
	eventsExchange = "quadracount.events"
	eventsQueue    = "balances.events"
	expenseCreated = "gasto.creado"
)

type event struct {
	EventID string          `json:"event_id"`
	Type    string          `json:"type"`
	GroupID string          `json:"group_id"`
	Data    json.RawMessage `json:"data"`
}

type expenseCreatedData struct {
	ExpenseID string         `json:"expense_id"`
	Amount    int64          `json:"amount"`
	PaidBy    string         `json:"paid_by"`
	Shares    []ledger.Share `json:"shares"`
}

// errInvalidEvent marca mensajes que nunca se van a poder procesar: se descartan.
var errInvalidEvent = errors.New("evento inválido")

// ConsumeEvents escucha balances.events y se reconecta solo si RabbitMQ se cae.
func ConsumeEvents(ctx context.Context, url string, store *Store) {
	for ctx.Err() == nil {
		if err := consumeOnce(ctx, url, store); err != nil {
			log.Printf("consumidor de eventos detenido: %v; reintento en 3 s", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func consumeOnce(ctx context.Context, url string, store *Store) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(eventsQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(eventsQueue, expenseCreated, eventsExchange, false, nil); err != nil {
		return err
	}
	if err := ch.Qos(10, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(eventsQueue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	log.Printf("escuchando %s", eventsQueue)

	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case amqpErr := <-closed:
			return fmt.Errorf("conexión cerrada: %v", amqpErr)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("el canal de mensajes se cerró")
			}
			handleDelivery(ctx, store, d)
		}
	}
}

func handleDelivery(ctx context.Context, store *Store, d amqp.Delivery) {
	err := processEvent(ctx, store, d.Body)
	switch {
	case err == nil, errors.Is(err, ErrAlreadyProcessed):
		if err != nil {
			log.Printf("evento repetido ignorado")
		}
		_ = d.Ack(false)
	case errors.Is(err, errInvalidEvent):
		log.Printf("descartado: %v", err)
		_ = d.Nack(false, false)
	default:
		log.Printf("error procesando evento, vuelve a la cola: %v", err)
		time.Sleep(time.Second) // evita reintentar en un ciclo apretado si Mongo está caído
		_ = d.Nack(false, true)
	}
}

func processEvent(ctx context.Context, store *Store, body []byte) error {
	var ev event
	if err := json.Unmarshal(body, &ev); err != nil {
		return fmt.Errorf("%w: %v", errInvalidEvent, err)
	}
	if ev.Type != expenseCreated {
		return nil // la cola solo está enlazada a gasto.creado, pero por si acaso
	}
	if ev.EventID == "" || ev.GroupID == "" {
		return fmt.Errorf("%w: falta event_id o group_id", errInvalidEvent)
	}
	var data expenseCreatedData
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		return fmt.Errorf("%w: %v", errInvalidEvent, err)
	}
	deltas, err := ledger.ExpenseDeltas(data.PaidBy, data.Amount, data.Shares)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidEvent, err)
	}
	if err := store.ApplyEvent(ctx, ev.EventID, ev.GroupID, deltas); err != nil {
		return err
	}
	log.Printf("gasto %s aplicado al grupo %s", data.ExpenseID, ev.GroupID)
	return nil
}
