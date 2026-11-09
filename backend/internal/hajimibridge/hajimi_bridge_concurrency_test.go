package hajimibridge

import (
	"context"
	"sync"
	"testing"

	"yingce/backend/internal/model"
)

// 同一幂等键并发提交只产生一笔划转；不同幂等键并发转回总额不超过 net_in。
func TestConcurrentTransfersNeverDoubleMoveMoney(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.create(t, "in", 10_000_000, idemKey("seed"))

	var wg sync.WaitGroup
	ids := make([]string, 8)
	for index := range ids {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			transfer, err := env.svc.CreateTransfer(ctx, env.user, CreateTransferRequest{Direction: "in", AmountMicro: 2_000_000, IdempotencyKey: idemKey("same-key")})
			if err == nil {
				ids[index] = transfer.ID
			}
		}(index)
	}
	wg.Wait()
	var first string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("same idempotency key produced different transfers: %v", ids)
		}
	}
	var rows int64
	env.db.Model(&model.HajimiBridgeTransfer{}).Where("client_key = ?", idemKey("same-key")).Count(&rows)
	if rows != 1 || env.fake.committedCount() != 2 {
		t.Fatalf("rows=%d committed=%d", rows, env.fake.committedCount())
	}

	// net_in = 12；6 个并发转回各 3，最多成功 4 笔。
	results := make([]string, 6)
	for index := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			transfer, err := env.svc.CreateTransfer(ctx, env.user, CreateTransferRequest{Direction: "out", AmountMicro: 3_000_000, IdempotencyKey: idemKey("out-" + string(rune('a'+index)))})
			if err == nil {
				results[index] = transfer.Status
			}
		}(index)
	}
	wg.Wait()
	completed := 0
	for _, status := range results {
		if status == model.HajimiTransferCompleted || status == model.HajimiTransferPending {
			completed++
		}
	}
	netIn, err := env.repo.HajimiBridgeLocalNetIn(env.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	available := env.available(t, env.user.ID)
	t.Logf("same-key results=%v withdraw results=%v", ids, results)
	if completed > 4 || netIn < 0 || available < 0 || available != 12_000_000-int64(completed)*3_000_000 {
		t.Fatalf("withdraws=%d netIn=%d available=%d results=%v", completed, netIn, available, results)
	}
}
