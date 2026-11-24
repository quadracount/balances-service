package main

import (
	"context"
	"sort"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/quadracount/balances-service/gen/balancespb"
	"github.com/quadracount/balances-service/ledger"
)

// server implementa balances.proto. No verifica membresía: el BFF y groups-service
// solo llaman con grupos ya verificados.
type server struct {
	pb.UnimplementedBalancesServiceServer
	store *Store
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

// RegisterPayment queda para la Fase 4 (por ahora responde UNIMPLEMENTED).

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
