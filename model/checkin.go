package model

import (
	"fmt"
	"math/rand"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Checkin 签到记录
type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	Cleared      bool   `json:"cleared" gorm:"default:false;index"`
}

// CheckinRecord 用于API返回的签到记录（不包含敏感字段）
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// UserCheckin 执行用户签到
// The record and reward are committed atomically on every supported database.
func UserCheckin(userId int) (*Checkin, error) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		return nil, common.NewMessage("Check-in feature is not enabled")
	}
	if setting.MinQuota <= 0 || setting.MaxQuota < setting.MinQuota || setting.MaxQuota > common.MaxWalletQuota {
		return nil, common.NewMessage("Invalid check-in reward quota configuration")
	}

	// 检查今天是否已签到
	hasChecked, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, err
	}
	if hasChecked {
		return nil, common.NewMessage("Already checked in today")
	}

	// 计算随机额度奖励
	quotaAwarded := setting.MinQuota
	if setting.MaxQuota > setting.MinQuota {
		quotaAwarded = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
	}

	today := time.Now().Format("2006-01-02")
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    time.Now().Unix(),
	}

	return userCheckinWithTransaction(checkin, userId, quotaAwarded)
}

// userCheckinWithTransaction commits a check-in and its restricted reward pool.
func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	if err := common.ValidateWalletQuota(quotaAwarded); err != nil {
		return nil, err
	}
	expiresAt := operation_setting.CalcBonusQuotaExpiresAt(time.Now().Unix())
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(checkin).Error; err != nil {
			return common.NewMessage("Check-in failed, please try again later")
		}
		_, err := CreateBonusQuotaGrantTx(tx, userId, BonusQuotaSourceCheckin, checkin.CheckinDate, quotaAwarded, expiresAt)
		if err != nil {
			return common.NewMessage("Check-in failed: quota update error")
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return checkin, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]any, error) {
	// 获取指定月份的所有签到记录
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := GetUserCheckinRecords(userId, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// 转换为不包含敏感字段的记录
	checkinRecords := make([]CheckinRecord, len(records))
	for i, r := range records {
		checkinRecords[i] = CheckinRecord{
			CheckinDate:  r.CheckinDate,
			QuotaAwarded: r.QuotaAwarded,
		}
	}

	// 检查今天是否已签到
	hasCheckedToday, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, err
	}

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	if err := DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota).Error; err != nil {
		return nil, err
	}

	bonusQuota, err := GetUserBonusQuotaTotal(userId)
	if err != nil {
		return nil, err
	}
	bonusGrants, err := GetUserBonusQuotaGrantSummaries(userId)
	if err != nil {
		return nil, err
	}
	bonusSetting := operation_setting.GetBonusQuotaSetting()

	return map[string]any{
		"total_quota":                totalQuota,
		"total_checkins":             totalCheckins,
		"checkin_count":              len(records),
		"checked_in_today":           hasCheckedToday,
		"records":                    checkinRecords,
		"bonus_quota":                bonusQuota,
		"bonus_quota_grants":         bonusGrants,
		"bonus_quota_validity_days":  bonusSetting.ValidityDays,
		"bonus_quota_allowed_models": bonusSetting.GetAllowedModelsList(),
	}, nil
}

type UserCheckinSum struct {
	UserId   int
	Username string
	Total    int
}

// GetCheckinQuotaSumByUsers 按用户分组统计未清除的签到额度（基于奖励额度池）
func GetCheckinQuotaSumByUsers(startDate, endDate string, userIds []int) ([]UserCheckinSum, error) {
	var sums []BonusQuotaUserSum
	query := DB.Model(&BonusQuotaGrant{}).
		Select("user_id, SUM(amount_remaining) AS total").
		Where("source = ? AND source_ref >= ? AND source_ref <= ? AND status = ? AND amount_remaining > 0",
			BonusQuotaSourceCheckin, startDate, endDate, BonusQuotaStatusActive).
		Where("expires_at = 0 OR expires_at > ?", time.Now().Unix()).
		Group("user_id").Order("user_id")
	if len(userIds) > 0 {
		query = query.Where("user_id IN ?", userIds)
	}
	if err := query.Find(&sums).Error; err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, nil
	}

	ids := make([]int, len(sums))
	for i, s := range sums {
		ids[i] = s.UserId
	}
	var users []User
	if err := DB.Unscoped().Where("id IN ?", ids).Select("id, username").Find(&users).Error; err != nil {
		return nil, err
	}
	usernameMap := make(map[int]string, len(users))
	for _, u := range users {
		usernameMap[u.Id] = u.Username
	}

	results := make([]UserCheckinSum, len(sums))
	for i, s := range sums {
		results[i] = UserCheckinSum{
			UserId:   s.UserId,
			Username: usernameMap[s.UserId],
			Total:    s.Total,
		}
	}
	return results, nil
}

type ClearCheckinPreviewItem struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	CheckinQuota int    `json:"checkin_quota"`
	ActualClear  int    `json:"actual_clear"`
}

// PreviewClearCheckinQuota 预览清除签到额度的影响
func PreviewClearCheckinQuota(startDate, endDate string, userIds []int) ([]ClearCheckinPreviewItem, error) {
	sums, err := GetCheckinQuotaSumByUsers(startDate, endDate, userIds)
	if err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, nil
	}

	items := make([]ClearCheckinPreviewItem, 0, len(sums))
	for _, s := range sums {
		items = append(items, ClearCheckinPreviewItem{
			UserId:       s.UserId,
			Username:     s.Username,
			CheckinQuota: s.Total,
			ActualClear:  s.Total,
		})
	}
	return items, nil
}

// BatchClearCheckinQuota 批量清除签到奖励额度
func BatchClearCheckinQuota(startDate, endDate string, userIds []int, _ string) (affectedUsers int, totalCleared int, err error) {
	cleared := make(map[int]int)
	err = DB.Transaction(func(tx *gorm.DB) error {
		// Write first so SQLite takes its writer lock before reading balances.
		markQuery := tx.Model(&Checkin{}).Where("checkin_date >= ? AND checkin_date <= ?", startDate, endDate)
		if len(userIds) > 0 {
			markQuery = markQuery.Where("user_id IN ?", userIds)
		}
		if err := markQuery.Update("cleared", true).Error; err != nil {
			return fmt.Errorf("标记签到记录失败: %w", err)
		}
		var grants []BonusQuotaGrant
		query := lockForUpdate(tx).Where("source = ? AND source_ref >= ? AND source_ref <= ? AND status IN ?",
			BonusQuotaSourceCheckin, startDate, endDate, []string{BonusQuotaStatusActive, BonusQuotaStatusDepleted}).Order("id")
		if len(userIds) > 0 {
			query = query.Where("user_id IN ?", userIds)
		}
		if err := query.Find(&grants).Error; err != nil {
			return err
		}
		now := time.Now().Unix()
		for _, grant := range grants {
			if err := tx.Model(&BonusQuotaGrant{}).Where("id = ?", grant.Id).
				Updates(map[string]any{"amount_remaining": 0, "status": BonusQuotaStatusCleared}).Error; err != nil {
				return err
			}
			if grant.AmountRemaining > 0 && (grant.ExpiresAt == 0 || grant.ExpiresAt > now) {
				cleared[grant.UserId] += grant.AmountRemaining
				totalCleared += grant.AmountRemaining
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	ids := make([]int, 0, len(cleared))
	for userID := range cleared {
		ids = append(ids, userID)
	}
	slices.Sort(ids)
	for _, userID := range ids {
		// Operator details belong to the route's independent administrator audit.
		RecordLogWithQuota(userID, LogTypeSystem,
			common.NewMessage("Cleared check-in rewards from {{start}} to {{end}}, quota: {{quota}}", map[string]any{
				"start": startDate, "end": endDate, "quota": logger.FormatQuota(cleared[userID]),
			}), -cleared[userID])
	}
	return len(cleared), totalCleared, nil
}
