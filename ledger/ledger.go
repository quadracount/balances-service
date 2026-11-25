// Package ledger tiene la aritmética de saldos, sin dependencias de MongoDB ni RabbitMQ.
// Todos los montos son pesos chilenos enteros.
package ledger

import (
	"errors"
	"sort"
)

// Share es la parte de un gasto que le corresponde a un miembro.
type Share struct {
	UserID string `json:"user_id"`
	Amount int64  `json:"amount"`
}

// Transfer es un pago sugerido para saldar cuentas.
type Transfer struct {
	From   string
	To     string
	Amount int64
}

// ExpenseDeltas devuelve cuánto cambia el saldo neto de cada miembro por un gasto:
// quien pagó suma el total y cada participante resta su parte. La suma de los cambios es 0.
func ExpenseDeltas(paidBy string, amount int64, shares []Share) (map[string]int64, error) {
	if paidBy == "" {
		return nil, errors.New("falta paid_by")
	}
	if amount <= 0 {
		return nil, errors.New("el monto debe ser mayor que 0")
	}
	var sum int64
	deltas := map[string]int64{paidBy: amount}
	for _, s := range shares {
		if s.UserID == "" || s.Amount < 0 {
			return nil, errors.New("parte inválida")
		}
		sum += s.Amount
		deltas[s.UserID] -= s.Amount
	}
	if sum != amount {
		return nil, errors.New("las partes no suman el total")
	}
	return deltas, nil
}

// ValidatePayment revisa un pago entre dos miembros.
func ValidatePayment(from, to string, amount int64) error {
	if from == "" || to == "" {
		return errors.New("faltan quien paga o quien recibe")
	}
	if from == to {
		return errors.New("quien paga y quien recibe tienen que ser personas distintas")
	}
	if amount <= 0 {
		return errors.New("el monto debe ser mayor que 0")
	}
	return nil
}

// PaymentDeltas: quien paga queda con más saldo a favor y quien recibe, con menos.
func PaymentDeltas(from, to string, amount int64) map[string]int64 {
	return map[string]int64{from: amount, to: -amount}
}

type position struct {
	user   string
	amount int64
}

// SettlementPlan empareja repetidamente al que más debe con al que más le deben,
// transfiriendo el menor de los dos montos, hasta que todos quedan en 0.
// Los empates se resuelven por user_id para que el resultado sea siempre el mismo.
func SettlementPlan(balances map[string]int64) []Transfer {
	var debtors, creditors []position
	for user, net := range balances {
		switch {
		case net < 0:
			debtors = append(debtors, position{user, -net})
		case net > 0:
			creditors = append(creditors, position{user, net})
		}
	}

	transfers := []Transfer{}
	for len(debtors) > 0 && len(creditors) > 0 {
		sortPositions(debtors)
		sortPositions(creditors)
		d, c := &debtors[0], &creditors[0]
		amount := min(d.amount, c.amount)
		transfers = append(transfers, Transfer{From: d.user, To: c.user, Amount: amount})
		d.amount -= amount
		c.amount -= amount
		if d.amount == 0 {
			debtors = debtors[1:]
		}
		if c.amount == 0 {
			creditors = creditors[1:]
		}
	}
	return transfers
}

func sortPositions(p []position) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].amount != p[j].amount {
			return p[i].amount > p[j].amount
		}
		return p[i].user < p[j].user
	})
}
