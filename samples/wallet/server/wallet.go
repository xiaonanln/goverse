package main

import (
	"context"
	"sync"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/wallet/proto"
)

// Wallet is an in-memory balance object. Clients drive state mutations via
// reliable calls so that client-side retries are exactly-once: the same
// call_id always maps to the same applied side-effect.
type Wallet struct {
	goverseapi.BaseObject

	mu      sync.Mutex
	balance int64
}

func (w *Wallet) OnCreated() {
	w.Logger.Infof("Wallet %s created", w.Id())
	w.balance = 0
}

func (w *Wallet) Deposit(ctx context.Context, req *pb.DepositRequest) (*pb.WalletResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if req.Amount <= 0 {
		return &pb.WalletResponse{
			WalletId: w.Id(),
			Balance:  w.balance,
			Ok:       false,
			Reason:   "amount must be positive",
		}, nil
	}

	w.balance += req.Amount
	return &pb.WalletResponse{
		WalletId: w.Id(),
		Balance:  w.balance,
		Ok:       true,
	}, nil
}

func (w *Wallet) Withdraw(ctx context.Context, req *pb.WithdrawRequest) (*pb.WalletResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if req.Amount <= 0 {
		return &pb.WalletResponse{
			WalletId: w.Id(),
			Balance:  w.balance,
			Ok:       false,
			Reason:   "amount must be positive",
		}, nil
	}
	if w.balance < req.Amount {
		return &pb.WalletResponse{
			WalletId: w.Id(),
			Balance:  w.balance,
			Ok:       false,
			Reason:   "insufficient funds",
		}, nil
	}

	w.balance -= req.Amount
	return &pb.WalletResponse{
		WalletId: w.Id(),
		Balance:  w.balance,
		Ok:       true,
	}, nil
}

func (w *Wallet) GetBalance(ctx context.Context, req *pb.GetBalanceRequest) (*pb.WalletResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return &pb.WalletResponse{
		WalletId: w.Id(),
		Balance:  w.balance,
		Ok:       true,
	}, nil
}
