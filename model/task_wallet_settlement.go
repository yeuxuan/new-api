package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ApplyTaskWalletQuota commits a wallet task's final charge and all primary-DB
// accounting together. A stale caller cannot repeat the same adjustment.
func ApplyTaskWalletQuota(task *Task, actualQuota int) (bool, error) {
	if task == nil || task.ID == 0 || actualQuota < 0 || actualQuota > common.MaxQuota {
		return false, errors.New("invalid task wallet settlement")
	}
	expectedQuota := task.Quota
	var current Task
	var token Token
	var applied bool
	var walletDelta, quotaDelta int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.UserId != task.UserId || current.PrivateData.BillingSource == "subscription" {
			return errors.New("task wallet funding identity mismatch")
		}
		if current.Quota != expectedQuota {
			return nil
		}
		if current.Quota == actualQuota {
			if task.PrivateData.BillingContext != nil {
				current.PrivateData.BillingContext = task.PrivateData.BillingContext
				if err := tx.Model(&Task{}).Where("id = ? AND quota = ?", current.ID, current.Quota).
					Update("private_data", current.PrivateData).Error; err != nil {
					return err
				}
			}
			return nil
		}
		// Polling evaluates completion facts after the terminal CAS. Carry that
		// winner's frozen billing snapshot into the same transaction as the
		// wallet adjustment instead of losing it to the stale row reload above.
		if task.PrivateData.BillingContext != nil {
			current.PrivateData.BillingContext = task.PrivateData.BillingContext
		}
		if current.Quota < 0 || current.Quota > common.MaxQuota {
			return errors.New("invalid persisted task quota")
		}
		bonusTotal := 0
		for _, d := range current.PrivateData.BonusQuotaDeducts {
			if d.GrantId <= 0 || d.Amount <= 0 || d.Amount > current.Quota-bonusTotal {
				return errors.New("invalid persisted task bonus deductions")
			}
			bonusTotal += d.Amount
		}
		// The guarded write also acquires SQLite's write lock before moving funds.
		claim := tx.Model(&Task{}).Where("id = ? AND quota = ?", current.ID, expectedQuota).Update("quota", actualQuota)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return errors.New("task quota changed during settlement")
		}
		quotaDelta = actualQuota - current.Quota
		if quotaDelta > 0 {
			modelName := current.Properties.OriginModelName
			if bc := current.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
				modelName = bc.OriginModelName
			}
			deductions, bonusUsed, err := consumeBonusQuotaTx(tx, current.UserId, modelName, quotaDelta)
			if err != nil {
				return err
			}
			current.PrivateData.BonusQuotaDeducts = append(current.PrivateData.BonusQuotaDeducts, deductions...)
			walletDelta = -(quotaDelta - bonusUsed)
		} else {
			bonusRefund := min(-quotaDelta, bonusTotal)
			remaining := bonusRefund
			var refunds, deductions []BonusQuotaDeduction
			for _, d := range current.PrivateData.BonusQuotaDeducts {
				amount := min(remaining, d.Amount)
				if amount > 0 {
					refunds = append(refunds, BonusQuotaDeduction{GrantId: d.GrantId, Amount: amount})
				}
				if d.Amount > amount {
					deductions = append(deductions, BonusQuotaDeduction{GrantId: d.GrantId, Amount: d.Amount - amount})
				}
				remaining -= amount
			}
			if err := refundBonusQuotaTx(tx, current.UserId, refunds); err != nil {
				return err
			}
			current.PrivateData.BonusQuotaDeducts = deductions
			walletDelta = -quotaDelta - bonusRefund
		}
		userUpdate := tx.Model(&User{}).Where("id = ?", current.UserId)
		if walletDelta > 0 {
			userUpdate = userUpdate.Where("quota <= ?", common.MaxWalletQuota-walletDelta)
		} else if walletDelta < 0 {
			userUpdate = userUpdate.Where("quota >= ?", -common.MaxWalletQuota-walletDelta)
		}
		result := userUpdate.Updates(map[string]any{
			"quota": gorm.Expr("quota + ?", walletDelta), "used_quota": gorm.Expr("used_quota + ?", quotaDelta),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task wallet update failed: user missing or quota limit exceeded")
		}
		if current.PrivateData.TokenId > 0 {
			err := tx.First(&token, current.PrivateData.TokenId).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				if token.UserId != current.UserId {
					return errors.New("task token owner mismatch")
				}
				if err := tx.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
					"remain_quota":  gorm.Expr("remain_quota - ?", quotaDelta),
					"used_quota":    gorm.Expr("used_quota + ?", quotaDelta),
					"accessed_time": common.GetTimestamp(),
				}).Error; err != nil {
					return err
				}
			}
		}
		if current.ChannelId > 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", current.ChannelId).
				Update("used_quota", gorm.Expr("used_quota + ?", quotaDelta)).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Task{}).Where("id = ?", current.ID).Update("private_data", current.PrivateData).Error; err != nil {
			return err
		}
		current.Quota = actualQuota
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	task.Quota = current.Quota
	task.PrivateData.BonusQuotaDeducts = current.PrivateData.BonusQuotaDeducts
	if applied {
		if err := cacheIncrUserQuota(current.UserId, int64(walletDelta)); err != nil {
			common.SysError(fmt.Sprintf("task wallet cache update failed task=%s: %v", current.TaskID, err))
		}
		if common.RedisEnabled && token.Id > 0 {
			if _, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -int64(quotaDelta)); err != nil {
				common.SysError(fmt.Sprintf("task token cache update failed task=%s: %v", current.TaskID, err))
			}
		}
	}
	return applied, nil
}
