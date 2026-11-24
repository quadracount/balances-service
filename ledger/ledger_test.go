package ledger

import (
	"reflect"
	"testing"
)

const (
	martin = "u-martin"
	camila = "u-camila"
	sofia  = "u-sofia"
	diego  = "u-diego"
)

func apply(t *testing.T, balances map[string]int64, deltas map[string]int64) {
	t.Helper()
	for user, d := range deltas {
		balances[user] += d
	}
}

func mustExpense(t *testing.T, paidBy string, amount int64, shares []Share) map[string]int64 {
	t.Helper()
	deltas, err := ExpenseDeltas(paidBy, amount, shares)
	if err != nil {
		t.Fatalf("ExpenseDeltas: %v", err)
	}
	return deltas
}

// Datos de ejemplo del grupo "Depto Los Carrera 1450" (sección 1 del plan).
func deptoBalances(t *testing.T) map[string]int64 {
	balances := map[string]int64{}
	apply(t, balances, mustExpense(t, martin, 45000, []Share{
		{martin, 12500}, {camila, 12500}, {sofia, 12500}, {diego, 7500},
	}))
	apply(t, balances, mustExpense(t, camila, 24000, []Share{
		{martin, 6000}, {camila, 6000}, {sofia, 6000}, {diego, 6000},
	}))
	apply(t, balances, mustExpense(t, sofia, 18000, []Share{
		{martin, 4500}, {camila, 4500}, {sofia, 4500}, {diego, 4500},
	}))
	apply(t, balances, PaymentDeltas(diego, martin, 10000))
	return balances
}

func TestSaldosDepto(t *testing.T) {
	got := deptoBalances(t)
	want := map[string]int64{martin: 12000, camila: 1000, sofia: -5000, diego: -8000}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saldos = %v, se esperaba %v", got, want)
	}
	var sum int64
	for _, net := range got {
		sum += net
	}
	if sum != 0 {
		t.Fatalf("la suma de los saldos es %d, debería ser 0", sum)
	}
}

func TestPlanDepto(t *testing.T) {
	got := SettlementPlan(deptoBalances(t))
	want := []Transfer{
		{From: diego, To: martin, Amount: 8000},
		{From: sofia, To: martin, Amount: 4000},
		{From: sofia, To: camila, Amount: 1000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %v, se esperaba %v", got, want)
	}
}

func TestProyectoIngenieria(t *testing.T) {
	balances := map[string]int64{}
	apply(t, balances, mustExpense(t, "u-valentina", 10500, []Share{
		{martin, 3500}, {"u-valentina", 3500}, {"u-tomas", 3500},
	}))
	if balances[martin] != -3500 {
		t.Fatalf("Martín debería deber 3500, tiene %d", balances[martin])
	}
}

func TestPlanVacioSiTodosEstanAlDia(t *testing.T) {
	got := SettlementPlan(map[string]int64{martin: 0, camila: 0})
	if len(got) != 0 {
		t.Fatalf("se esperaba un plan vacío, hay %v", got)
	}
}

func TestGastoConPartesQueNoSuman(t *testing.T) {
	if _, err := ExpenseDeltas(martin, 1000, []Share{{martin, 500}}); err == nil {
		t.Fatal("se esperaba un error")
	}
}
