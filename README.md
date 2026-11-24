# balances-service

Saldos, pagos y plan para saldar cuentas. Go + MongoDB (`balances_db`).

No guarda gastos: se entera de ellos por el evento `gasto.creado`.

## gRPC (puerto 50052)

El contrato es `protos/balances.proto`. El BFF y groups-service tienen una copia que tiene que ser idéntica:

```bash
# desde la carpeta con todos los repos
diff balances-service/protos/balances.proto bff-gateway/protos/balances.proto
diff balances-service/protos/balances.proto groups-service/protos/balances.proto
```

- `GetGroupBalances` y `GetSettlementPlan`: los usa el BFF.
- `GetMemberBalance`: lo usa groups-service antes de quitar a un miembro.
- `RegisterPayment`: lo usa el BFF. **Pendiente (Fase 4)**, por ahora responde `UNIMPLEMENTED`. Recibe también `from_name` y `to_name`: balances no conoce los nombres de los miembros y los necesita para el evento `pago.registrado`. El BFF los saca del grupo que ya verificó groups-service.

También expone `grpc.health.v1` para los probes de Kubernetes.

El código está separado en `ledger/` (la aritmética de saldos y el plan, sin dependencias, con los tests), `store.go` (MongoDB), `events.go` (RabbitMQ, se reconecta solo) y `server.go` (gRPC).

El código generado está en `gen/balancespb/` y se commitea. Si cambia el `.proto`, se regenera con `./scripts/gen-proto.sh` (necesita `protoc`, `protoc-gen-go` y `protoc-gen-go-grpc`).

## Eventos

Consume `gasto.creado` desde la cola `balances.events`:

```json
{
  "event_id": "uuid",
  "type": "gasto.creado",
  "occurred_at": "2026-11-06T18:20:00Z",
  "group_id": "g-depto",
  "data": {
    "expense_id": "e-1",
    "description": "Gas",
    "amount": 18000,
    "paid_by": "u-sofia",
    "paid_by_name": "Sofía",
    "shares": [{ "user_id": "u-martin", "amount": 4500 }],
    "source": "MANUAL"
  }
}
```

Antes de aplicarlo se guarda el `event_id` en `processed_events`. Si ya estaba, se ignora, así un mensaje repetido no altera los saldos. Si después falla el `$inc`, se borra ese registro y el mensaje vuelve a la cola. El `ack` se hace después de procesar. Un evento mal formado (o cuyas partes no suman el total) se descarta.

Publica `pago.registrado` con `payment_id`, `from_user_id`, `from_name`, `to_user_id`, `to_name` y `amount` (los nombres vienen en `RegisterPaymentRequest`).

## Tests

Con los datos de ejemplo del grupo "Depto Los Carrera 1450" los saldos tienen que dar Martín +12.000, Camila +1.000, Sofía −5.000 y Diego −8.000, y el plan: Diego → Martín $8.000, Sofía → Martín $4.000, Sofía → Camila $1.000.

## Variables de entorno

| Variable | Ejemplo |
|---|---|
| `GRPC_PORT` | `50052` |
| `MONGO_URL` | `mongodb://mongodb:27017` |
| `MONGO_DB` | `balances_db` |
| `RABBITMQ_URL` | `amqp://rabbitmq:5672` |

## Correr

```bash
go vet ./...
go test ./...
go build ./...
```
