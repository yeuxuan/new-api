package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

const (
	BonusQuotaSourceEmailBind = "email_bind"
	BonusQuotaSourceCheckin   = "checkin"

	BonusQuotaStatusActive   = "active"
	BonusQuotaStatusExpired  = "expired"
	BonusQuotaStatusDepleted = "depleted"
	BonusQuotaStatusCleared  = "cleared"
)

// BonusQuotaGrant 奖励额度发放记录
type BonusQuotaGrant struct {
	Id               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId           int    `json:"user_id" gorm:"not null;index:idx_bonus_quota_user_status_expires;uniqueIndex:idx_bonus_quota_user_source_ref"`
	Source           string `json:"source" gorm:"type:varchar(32);not null;uniqueIndex:idx_bonus_quota_user_source_ref"`
	SourceRef        string `json:"source_ref" gorm:"type:varchar(32);default:'';uniqueIndex:idx_bonus_quota_user_source_ref"`
	AmountTotal      int    `json:"amount_total" gorm:"not null"`
	AmountRemaining  int    `json:"amount_remaining" gorm:"not null"`
	ExpiresAt        int64  `json:"expires_at" gorm:"bigint;default:0;index:idx_bonus_quota_user_status_expires"`
	Status           string `json:"status" gorm:"type:varchar(16);not null;default:active;index:idx_bonus_quota_user_status_expires"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
}

func (BonusQuotaGrant) TableName() string {
	return "bonus_quota_grants"
}

// MigrateBonusQuotaGrantUniqueIndex drops the incorrect global unique index on (source, source_ref).
func MigrateBonusQuotaGrantUniqueIndex() error {
	if DB == nil || !DB.Migrator().HasTable(&BonusQuotaGrant{}) {
		return nil
	}
	m := DB.Migrator()
	if m.HasIndex(&BonusQuotaGrant{}, "idx_bonus_quota_source_ref") {
		if err := m.DropIndex(&BonusQuotaGrant{}, "idx_bonus_quota_source_ref"); err != nil {
			return fmt.Errorf("drop idx_bonus_quota_source_ref: %w", err)
		}
	}
	return nil
}

// EnsureBonusQuotaGrantUniqueIndex creates the per-user unique index when missing.
func EnsureBonusQuotaGrantUniqueIndex() error {
	if DB == nil || !DB.Migrator().HasTable(&BonusQuotaGrant{}) {
		return nil
	}
	m := DB.Migrator()
	if m.HasIndex(&BonusQuotaGrant{}, "idx_bonus_quota_user_source_ref") {
		return nil
	}
	if err := m.CreateIndex(&BonusQuotaGrant{}, "idx_bonus_quota_user_source_ref"); err != nil {
		return fmt.Errorf("create idx_bonus_quota_user_source_ref: %w", err)
	}
	return nil
}

// BonusQuotaGrantSummary API 返回摘要
type BonusQuotaGrantSummary struct {
	Id              int    `json:"id"`
	Source          string `json:"source"`
	AmountRemaining int    `json:"amount_remaining"`
	ExpiresAt       int64  `json:"expires_at"`
}

// BonusQuotaDeduction 单次消费明细，用于退款
type BonusQuotaDeduction struct {
	GrantId int
	Amount  int
}

func bonusQuotaModelAllowed(modelName string) bool {
	return operation_setting.IsModelAllowedForBonusQuota(modelName)
}

func activeBonusGrantQuery(userId int) *gorm.DB {
	now := time.Now().Unix()
	return DB.Model(&BonusQuotaGrant{}).
		Where("user_id = ? AND status = ? AND amount_remaining > 0", userId, BonusQuotaStatusActive).
		Where("(expires_at = 0 OR expires_at > ?)", now)
}

// HasEmailBindBonusGrant 检查用户是否已领取邮箱绑定奖励
func HasEmailBindBonusGrant(userId int) (bool, error) {
	var count int64
	err := DB.Model(&BonusQuotaGrant{}).
		Where("user_id = ? AND source = ?", userId, BonusQuotaSourceEmailBind).
		Count(&count).Error
	return count > 0, err
}

// CreateBonusQuotaGrant 创建奖励额度（非事务）
func CreateBonusQuotaGrant(userId int, source, sourceRef string, amount int, expiresAt int64) (*BonusQuotaGrant, error) {
	return createBonusQuotaGrant(DB, userId, source, sourceRef, amount, expiresAt)
}

// CreateBonusQuotaGrantTx 在事务中创建奖励额度
func CreateBonusQuotaGrantTx(tx *gorm.DB, userId int, source, sourceRef string, amount int, expiresAt int64) (*BonusQuotaGrant, error) {
	return createBonusQuotaGrant(tx, userId, source, sourceRef, amount, expiresAt)
}

func createBonusQuotaGrant(db *gorm.DB, userId int, source, sourceRef string, amount int, expiresAt int64) (*BonusQuotaGrant, error) {
	if amount <= 0 {
		return nil, errors.New("奖励额度必须大于 0")
	}
	grant := &BonusQuotaGrant{
		UserId:          userId,
		Source:          source,
		SourceRef:       sourceRef,
		AmountTotal:     amount,
		AmountRemaining: amount,
		ExpiresAt:       expiresAt,
		Status:          BonusQuotaStatusActive,
		CreatedAt:       time.Now().Unix(),
	}
	if err := db.Create(grant).Error; err != nil {
		return nil, err
	}
	return grant, nil
}

// GetAvailableBonusQuota 获取用户在指定模型下可用的奖励额度总量
func GetAvailableBonusQuota(userId int, modelName string) (int, error) {
	if !bonusQuotaModelAllowed(modelName) {
		return 0, nil
	}
	var total int64
	err := activeBonusGrantQuery(userId).Select("COALESCE(SUM(amount_remaining), 0)").Scan(&total).Error
	return int(total), err
}

// GetUserBonusQuotaTotal 获取用户全部可用奖励额度（不区分模型）
func GetUserBonusQuotaTotal(userId int) (int, error) {
	var total int64
	err := activeBonusGrantQuery(userId).Select("COALESCE(SUM(amount_remaining), 0)").Scan(&total).Error
	return int(total), err
}

// GetUserBonusQuotaGrantSummaries 获取用户奖励额度摘要列表
func GetUserBonusQuotaGrantSummaries(userId int) ([]BonusQuotaGrantSummary, error) {
	var grants []BonusQuotaGrant
	err := activeBonusGrantQuery(userId).
		Order("CASE WHEN expires_at = 0 THEN 1 ELSE 0 END, expires_at ASC, id ASC").
		Find(&grants).Error
	if err != nil {
		return nil, err
	}
	summaries := make([]BonusQuotaGrantSummary, len(grants))
	for i, g := range grants {
		summaries[i] = BonusQuotaGrantSummary{
			Id:              g.Id,
			Source:          g.Source,
			AmountRemaining: g.AmountRemaining,
			ExpiresAt:       g.ExpiresAt,
		}
	}
	return summaries, nil
}

// ConsumeBonusQuota 按先过期先扣（expires_at=0 最后）消费奖励额度。
// 若可用奖励不足 amount，则扣完即止（返回 consumed < amount），由调用方补扣钱包。
func ConsumeBonusQuota(userId int, modelName string, amount int) ([]BonusQuotaDeduction, int, error) {
	if amount <= 0 {
		return nil, 0, nil
	}
	if !bonusQuotaModelAllowed(modelName) {
		return nil, 0, nil
	}

	var deductions []BonusQuotaDeduction
	consumed := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		remaining := amount
		const maxRetries = 64
		for remaining > 0 {
			now := time.Now().Unix()
			var grant BonusQuotaGrant
			err := tx.Where("user_id = ? AND status = ? AND amount_remaining > 0", userId, BonusQuotaStatusActive).
				Where("(expires_at = 0 OR expires_at > ?)", now).
				Order("CASE WHEN expires_at = 0 THEN 1 ELSE 0 END, expires_at ASC, id ASC").
				First(&grant).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				break
			}
			if err != nil {
				return err
			}

			deduct := grant.AmountRemaining
			if deduct > remaining {
				deduct = remaining
			}
			newRemaining := grant.AmountRemaining - deduct
			status := BonusQuotaStatusActive
			if newRemaining == 0 {
				status = BonusQuotaStatusDepleted
			}

			applied := false
			for attempt := 0; attempt < maxRetries; attempt++ {
				result := tx.Model(&BonusQuotaGrant{}).
					Where("id = ? AND amount_remaining = ?", grant.Id, grant.AmountRemaining).
					Updates(map[string]interface{}{
						"amount_remaining": newRemaining,
						"status":           status,
					})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected > 0 {
					applied = true
					break
				}
				if err := tx.Where("id = ?", grant.Id).First(&grant).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						break
					}
					return err
				}
				if grant.AmountRemaining <= 0 {
					break
				}
				deduct = grant.AmountRemaining
				if deduct > remaining {
					deduct = remaining
				}
				newRemaining = grant.AmountRemaining - deduct
				status = BonusQuotaStatusActive
				if newRemaining == 0 {
					status = BonusQuotaStatusDepleted
				}
			}
			if !applied {
				return fmt.Errorf("奖励额度并发更新失败")
			}

			deductions = append(deductions, BonusQuotaDeduction{GrantId: grant.Id, Amount: deduct})
			consumed += deduct
			remaining -= deduct
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return deductions, consumed, nil
}

// RefundBonusQuota 退还奖励额度到指定 grant；若 grant 已被清除或已过期则退至普通钱包。
func RefundBonusQuota(deductions []BonusQuotaDeduction) error {
	if len(deductions) == 0 {
		return nil
	}
	walletRefunds := make(map[int]int)
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().Unix()
		for _, d := range deductions {
			if d.Amount <= 0 {
				continue
			}
			var grant BonusQuotaGrant
			if err := tx.Where("id = ?", d.GrantId).First(&grant).Error; err != nil {
				return err
			}
			if grant.Status == BonusQuotaStatusCleared ||
				(grant.ExpiresAt > 0 && grant.ExpiresAt <= now) {
				walletRefunds[grant.UserId] += d.Amount
				continue
			}
			newRemaining := grant.AmountRemaining + d.Amount
			if newRemaining > grant.AmountTotal {
				newRemaining = grant.AmountTotal
			}
			status := BonusQuotaStatusActive
			if newRemaining == 0 {
				status = BonusQuotaStatusDepleted
			}
			if err := tx.Model(&BonusQuotaGrant{}).Where("id = ?", d.GrantId).
				Updates(map[string]interface{}{
					"amount_remaining": newRemaining,
					"status":           status,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for userId, amount := range walletRefunds {
		if amount <= 0 {
			continue
		}
		if err := increaseUserQuota(userId, amount); err != nil {
			return err
		}
	}
	return nil
}

// ExpireDueBonusQuotas 批量标记过期奖励额度
func ExpireDueBonusQuotas(batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 300
	}
	now := time.Now().Unix()
	result := DB.Model(&BonusQuotaGrant{}).
		Where("status = ? AND expires_at > 0 AND expires_at <= ?", BonusQuotaStatusActive, now).
		Limit(batchSize).
		Updates(map[string]interface{}{
			"status":           BonusQuotaStatusExpired,
			"amount_remaining": 0,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

// ClearCheckinBonusGrantsByDateRange 按日期范围清除签到奖励额度
func ClearCheckinBonusGrantsByDateRange(startDate, endDate string, userIds []int) (int, error) {
	query := DB.Model(&BonusQuotaGrant{}).
		Where("source = ? AND source_ref >= ? AND source_ref <= ? AND status = ?",
			BonusQuotaSourceCheckin, startDate, endDate, BonusQuotaStatusActive)
	if len(userIds) > 0 {
		query = query.Where("user_id IN ?", userIds)
	}
	result := query.Updates(map[string]interface{}{
		"amount_remaining": 0,
		"status":           BonusQuotaStatusCleared,
	})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

type BonusQuotaUserSum struct {
	UserId int
	Total  int
}

// GetCheckinBonusQuotaSumByUsers 按用户统计日期范围内未清除的签到奖励剩余额度
func GetCheckinBonusQuotaSumByUsers(startDate, endDate string, userIds []int) ([]BonusQuotaUserSum, error) {
	var results []struct {
		UserId int `gorm:"column:user_id"`
		Total  int `gorm:"column:total"`
	}
	query := DB.Model(&BonusQuotaGrant{}).
		Select("user_id, COALESCE(SUM(amount_remaining), 0) as total").
		Where("source = ? AND source_ref >= ? AND source_ref <= ? AND status = ?",
			BonusQuotaSourceCheckin, startDate, endDate, BonusQuotaStatusActive).
		Group("user_id")
	if len(userIds) > 0 {
		query = query.Where("user_id IN ?", userIds)
	}
	if err := query.Find(&results).Error; err != nil {
		return nil, err
	}
	sums := make([]BonusQuotaUserSum, len(results))
	for i, r := range results {
		sums[i] = BonusQuotaUserSum{UserId: r.UserId, Total: r.Total}
	}
	return sums, nil
}
