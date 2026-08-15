package service

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// ---------------------------------------------------------------------------
// FundingSource — 资金来源接口（钱包 or 订阅）
// ---------------------------------------------------------------------------

// FundingSource 抽象了预扣费的资金来源。
type FundingSource interface {
	// Source 返回资金来源标识："wallet" 或 "subscription"
	Source() string
	// PreConsume 从该资金来源预扣 amount 额度
	PreConsume(amount int) error
	// Settle 根据差额调整资金来源（正数补扣，负数退还）
	Settle(delta int) error
	// Refund 退还所有预扣费
	Refund() error
}

// ---------------------------------------------------------------------------
// WalletFunding — 钱包资金来源实现（含奖励额度池）
// ---------------------------------------------------------------------------

// ErrInsufficientWalletQuota 钱包原子预扣失败（余额不足），未发生任何扣减。
// BillingSession 据此映射为 ErrorCodeInsufficientUserQuota，
// 使 wallet_first 等计费偏好可以回退到订阅。
var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

type WalletFunding struct {
	userId        int
	modelName     string
	consumed      int
	bonusConsumed int
	bonusDeducts  []model.BonusQuotaDeduction
	reserveStack  []walletConsumption
}

type walletConsumption struct {
	walletAmount int
	bonusDeducts []model.BonusQuotaDeduction
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

func (w *WalletFunding) consumeAmount(amount int, trackReserve, requireAvailableWallet bool) error {
	if amount <= 0 {
		return nil
	}

	var newDeducts []model.BonusQuotaDeduction
	newDeducts, bonusUsed, err := model.ConsumeBonusQuota(w.userId, w.modelName, amount)
	if err != nil {
		return err
	}
	if bonusUsed > 0 {
		w.bonusDeducts = append(w.bonusDeducts, newDeducts...)
		w.bonusConsumed += bonusUsed
	}

	walletAmount := amount - bonusUsed
	if walletAmount > 0 {
		if requireAvailableWallet {
			reserved, reserveErr := model.TryReserveUserQuota(w.userId, walletAmount)
			if reserveErr != nil {
				err = reserveErr
			} else if !reserved {
				err = ErrInsufficientWalletQuota
			}
		} else {
			err = model.DecreaseUserQuota(w.userId, walletAmount, false)
		}
		if err != nil {
			if len(newDeducts) > 0 {
				_ = model.RefundBonusQuota(newDeducts)
				w.bonusConsumed -= bonusUsed
				w.bonusDeducts = removeBonusDeductions(w.bonusDeducts, newDeducts)
			}
			return err
		}
		w.consumed += walletAmount
	}

	if trackReserve {
		w.reserveStack = append(w.reserveStack, walletConsumption{
			walletAmount: walletAmount,
			bonusDeducts: newDeducts,
		})
	}
	return nil
}

func removeBonusDeductions(all, remove []model.BonusQuotaDeduction) []model.BonusQuotaDeduction {
	if len(remove) == 0 {
		return all
	}
	removeMap := make(map[int]int, len(remove))
	for _, d := range remove {
		removeMap[d.GrantId] += d.Amount
	}
	result := make([]model.BonusQuotaDeduction, 0, len(all))
	for _, d := range all {
		if rem, ok := removeMap[d.GrantId]; ok {
			if d.Amount > rem {
				result = append(result, model.BonusQuotaDeduction{GrantId: d.GrantId, Amount: d.Amount - rem})
			}
			continue
		}
		result = append(result, d)
	}
	return result
}

func (w *WalletFunding) PreConsume(amount int) error {
	return w.consumeAmount(amount, false, true)
}

func (w *WalletFunding) ReserveAdditional(delta int) error {
	return w.consumeAmount(delta, true, false)
}

func (w *WalletFunding) RollbackLastReserve() {
	if len(w.reserveStack) == 0 {
		return
	}
	last := w.reserveStack[len(w.reserveStack)-1]
	w.reserveStack = w.reserveStack[:len(w.reserveStack)-1]
	if last.walletAmount > 0 {
		_ = model.IncreaseUserQuota(w.userId, last.walletAmount, false)
		w.consumed -= last.walletAmount
	}
	if len(last.bonusDeducts) > 0 {
		_ = model.RefundBonusQuota(last.bonusDeducts)
		bonusTotal := 0
		for _, d := range last.bonusDeducts {
			bonusTotal += d.Amount
		}
		w.bonusConsumed -= bonusTotal
		w.bonusDeducts = removeBonusDeductions(w.bonusDeducts, last.bonusDeducts)
	}
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return w.consumeAmount(delta, false, false)
	}
	refundAmount := -delta
	bonusRefund := 0
	if w.bonusConsumed > 0 && refundAmount > 0 {
		if refundAmount > w.bonusConsumed {
			bonusRefund = w.bonusConsumed
		} else {
			bonusRefund = refundAmount
		}
	}
	walletRefund := refundAmount - bonusRefund

	if bonusRefund > 0 {
		refundDeducts := scaleBonusDeductions(w.bonusDeducts, bonusRefund)
		if err := model.RefundBonusQuota(refundDeducts); err != nil {
			return err
		}
		w.bonusConsumed -= bonusRefund
		w.bonusDeducts = shrinkBonusDeductions(w.bonusDeducts, bonusRefund)
	}
	if walletRefund > 0 {
		return model.IncreaseUserQuota(w.userId, walletRefund, false)
	}
	return nil
}

func (w *WalletFunding) Refund() error {
	if w.bonusConsumed > 0 {
		if err := model.RefundBonusQuota(w.bonusDeducts); err != nil {
			return err
		}
		w.bonusDeducts = nil
		w.bonusConsumed = 0
	}
	if w.consumed <= 0 {
		return nil
	}
	return model.IncreaseUserQuota(w.userId, w.consumed, false)
}

func scaleBonusDeductions(deductions []model.BonusQuotaDeduction, amount int) []model.BonusQuotaDeduction {
	result := make([]model.BonusQuotaDeduction, 0, len(deductions))
	remaining := amount
	for _, d := range deductions {
		if remaining <= 0 {
			break
		}
		refund := d.Amount
		if refund > remaining {
			refund = remaining
		}
		result = append(result, model.BonusQuotaDeduction{GrantId: d.GrantId, Amount: refund})
		remaining -= refund
	}
	return result
}

func shrinkBonusDeductions(deductions []model.BonusQuotaDeduction, refunded int) []model.BonusQuotaDeduction {
	remaining := refunded
	result := make([]model.BonusQuotaDeduction, 0, len(deductions))
	for _, d := range deductions {
		if d.Amount <= remaining {
			remaining -= d.Amount
			continue
		}
		if remaining > 0 {
			result = append(result, model.BonusQuotaDeduction{GrantId: d.GrantId, Amount: d.Amount - remaining})
			remaining = 0
		} else {
			result = append(result, d)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// SubscriptionFunding — 订阅资金来源实现
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId       string
	userId          int
	modelName       string
	amount          int64
	subscriptionId  int
	preConsumed     int64
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(_ int) error {
	res, err := model.PreConsumeUserSubscription(s.requestId, s.userId, s.modelName, 0, s.amount)
	if err != nil {
		return err
	}
	s.subscriptionId = res.UserSubscriptionId
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	if planInfo, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(res.UserSubscriptionId); err == nil && planInfo != nil {
		s.PlanId = planInfo.PlanId
		s.PlanTitle = planInfo.PlanTitle
	}
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	return model.PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta))
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 {
		return nil
	}
	return refundWithRetry(func() error {
		return model.RefundSubscriptionPreConsume(s.requestId)
	})
}

func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
