package main

import (
	"context"
	"log"
	"sort"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/quadracount/balances-service/gen/balancespb"
	"github.com/quadracount/balances-service/ledger"
)

// server implementa balances.proto. No verifica membresía: el BFF y groups-service
// solo llaman con grupos ya verificados.
type server struct {
	pb.UnimplementedBalancesServiceServer
	store     *Store
	publisher *Publisher
}

func (s *server) GetGroupBalances(ctx context.Context, req *pb.GroupRequest) (*pb.GroupBalances, error) {
	members, err := s.load(ctx, req.GetGroupId())
	if err != nil {
		return nil, err
	}
	users := make([]string, 0, len(members))
	for user := range members {
		users = append(users, user)
	}
	sort.Strings(users)

	res := &pb.GroupBalances{GroupId: req.GetGroupId()}
	for _, user := range users {
		res.Balances = append(res.Balances, &pb.MemberBalance{UserId: user, Net: members[user]})
	}
	return res, nil
}

func (s *server) GetSettlementPlan(ctx context.Context, req *pb.GroupRequest) (*pb.SettlementPlan, error) {
	members, err := s.load(ctx, req.GetGroupId())
	if err != nil {
		return nil, err
	}
	res := &pb.SettlementPlan{GroupId: req.GetGroupId()}
	for _, t := range ledger.SettlementPlan(members) {
		res.Transfers = append(res.Transfers, &pb.Transfer{FromUserId: t.From, ToUserId: t.To, Amount: t.Amount})
	}
	return res, nil
}

func (s *server) GetMemberBalance(ctx context.Context, req *pb.MemberBalanceRequest) (*pb.MemberBalance, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "falta user_id")
	}
	members, err := s.load(ctx, req.GetGroupId())
	if err != nil {
		return nil, err
	}
	return &pb.MemberBalance{UserId: req.GetUserId(), Net: members[req.GetUserId()]}, nil
}

// RegisterPayment guarda el pago, actualiza los saldos y publica pago.registrado.
// El BFF ya verificó que ambos sean miembros y que quien pide participe en el pago.
func (s *server) RegisterPayment(ctx context.Context, req *pb.RegisterPaymentRequest) (*pb.Payment, error) {
	if req.GetGroupId() == "" {
		return nil, status.Error(codes.InvalidArgument, "falta group_id")
	}
	if err := ledger.ValidatePayment(req.GetFromUserId(), req.GetToUserId(), req.GetAmount()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	p := Payment{
		ID:         newID(),
		GroupID:    req.GetGroupId(),
		FromUserID: req.GetFromUserId(),
		ToUserID:   req.GetToUserId(),
		Amount:     req.GetAmount(),
		CreatedAt:  time.Now().UTC(),
	}
	deltas := ledger.PaymentDeltas(p.FromUserID, p.ToUserID, p.Amount)
	if err := s.store.RecordPayment(ctx, p, deltas); err != nil {
		return nil, status.Errorf(codes.Unavailable, "no se pudo registrar el pago: %v", err)
	}

	// El pago ya quedó guardado. El evento se envía en segundo plano (y se reintenta
	// si RabbitMQ está caído), así que no hace esperar la respuesta.
	err := s.publisher.Publish("pago.registrado", p.GroupID, map[string]any{
		"payment_id":   p.ID,
		"from_user_id": p.FromUserID,
		"from_name":    nameOr(req.GetFromName(), p.FromUserID),
		"to_user_id":   p.ToUserID,
		"to_name":      nameOr(req.GetToName(), p.ToUserID),
		"amount":       p.Amount,
	})
	if err != nil {
		log.Printf("pago %s guardado, pero no se pudo encolar el evento: %v", p.ID, err)
	}

	return &pb.Payment{
		Id:         p.ID,
		GroupId:    p.GroupID,
		FromUserId: p.FromUserID,
		ToUserId:   p.ToUserID,
		Amount:     p.Amount,
		CreatedAt:  p.CreatedAt.Format(time.RFC3339Nano),
	}, nil
}

func nameOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

func (s *server) load(ctx context.Context, groupID string) (map[string]int64, error) {
	if groupID == "" {
		return nil, status.Error(codes.InvalidArgument, "falta group_id")
	}
	members, err := s.store.GroupBalances(ctx, groupID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "no se pudieron leer los saldos: %v", err)
	}
	return members, nil
}
