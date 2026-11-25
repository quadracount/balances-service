package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const pendingLimit = 1000

// Publisher publica eventos en el exchange quadracount.events.
//
// Publish no espera a RabbitMQ: deja el evento en una cola en memoria y una
// goroutine lo envía. Si RabbitMQ está caído, reintenta cada 3 s hasta poder
// enviarlo (si el proceso se reinicia antes, los eventos pendientes se pierden).
type Publisher struct {
	url     string
	pending chan pendingEvent
}

type pendingEvent struct {
	routingKey string
	body       []byte
}

type outgoingEvent struct {
	EventID    string `json:"event_id"`
	Type       string `json:"type"`
	OccurredAt string `json:"occurred_at"`
	GroupID    string `json:"group_id"`
	Data       any    `json:"data"`
}

func NewPublisher(ctx context.Context, url string) *Publisher {
	p := &Publisher{url: url, pending: make(chan pendingEvent, pendingLimit)}
	go p.run(ctx)
	return p
}

// Publish arma el evento con el formato común de QuadraCount y lo deja para enviar.
func (p *Publisher) Publish(eventType, groupID string, data any) error {
	body, err := json.Marshal(outgoingEvent{
		EventID:    newID(),
		Type:       eventType,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		GroupID:    groupID,
		Data:       data,
	})
	if err != nil {
		return err
	}
	select {
	case p.pending <- pendingEvent{routingKey: eventType, body: body}:
		return nil
	default:
		return errors.New("hay demasiados eventos pendientes de enviar")
	}
}

func (p *Publisher) run(ctx context.Context) {
	var conn *amqp.Connection
	var ch *amqp.Channel
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	for {
		var ev pendingEvent
		select {
		case <-ctx.Done():
			return
		case ev = <-p.pending:
		}

		// Reintenta el mismo evento hasta enviarlo
		for {
			if ch == nil || ch.IsClosed() {
				if conn != nil {
					_ = conn.Close()
				}
				var err error
				conn, ch, err = connectPublisher(p.url)
				if err != nil {
					log.Printf("RabbitMQ no disponible para publicar (%v); reintento en 3 s", err)
					if !sleepCtx(ctx, 3*time.Second) {
						return
					}
					continue
				}
			}
			err := ch.PublishWithContext(ctx, eventsExchange, ev.routingKey, false, false, amqp.Publishing{
				ContentType:  "application/json",
				DeliveryMode: amqp.Persistent,
				Body:         ev.body,
			})
			if err == nil {
				log.Printf("publicado %s", ev.routingKey)
				break
			}
			log.Printf("error publicando %s (%v); reintento en 3 s", ev.routingKey, err)
			_ = conn.Close()
			ch = nil
			if !sleepCtx(ctx, 3*time.Second) {
				return
			}
		}
	}
}

func connectPublisher(url string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if err := declareTopology(ch); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ch, nil
}

// declareTopology declara el exchange y las colas de TODOS los consumidores con
// sus bindings. Lo hacen los que publican (este servicio y groups-service), no solo
// los consumidores: un evento que llega a un exchange sin colas enlazadas se
// descarta, así que las colas tienen que existir antes del primer evento aunque el
// consumidor todavía no haya arrancado. Declarar algo que ya existe con las mismas
// opciones no tiene efecto.
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	queues := []struct{ name, binding string }{
		{eventsQueue, expenseCreated}, // balances.events <- gasto.creado
		{"activity.events", "#"},      // activity.events <- todos los eventos
	}
	for _, q := range queues {
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind(q.name, q.binding, eventsExchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// newID genera un UUID v4 aleatorio.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
