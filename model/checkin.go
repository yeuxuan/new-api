package model

import (
	"errors"
	"fmt"
	"math/rand"
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
// MySQL 和 PostgreSQL 使用事务保证原子性
// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
func UserCheckin(userId int) (*Checkin, error) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		return nil, errors.New("签到功能未启用")
	}

	// 检查今天是否已签到
	hasChecked, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, err
	}
	if hasChecked {
		return nil, errors.New("今日已签到")
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

	// 根据数据库类型选择不同的策略
	if common.UsingSQLite {
		// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
		return userCheckinWithoutTransaction(checkin, userId, quotaAwarded)
	}

	// MySQL 和 PostgreSQL 支持事务，使用事务保证原子性
	return userCheckinWithTransaction(checkin, userId, quotaAwarded)
}

// userCheckinWithTransaction 使用事务执行签到（适用于 MySQL 和 PostgreSQL）
func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 步骤1: 创建签到记录
		// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}

		// 步骤2: 在事务中增加用户额度
		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error; err != nil {
			return errors.New("签到失败：更新额度出错")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 事务成功后，异步更新缓存
	go func() {
		_ = cacheIncrUserQuota(userId, int64(quotaAwarded))
	}()

	return checkin, nil
}

// userCheckinWithoutTransaction 不使用事务执行签到（适用于 SQLite）
func userCheckinWithoutTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	// 步骤1: 创建签到记录
	// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
	if err := DB.Create(checkin).Error; err != nil {
		return nil, errors.New("签到失败，请稍后重试")
	}

	// 步骤2: 增加用户额度
	// 使用 db=true 强制直接写入数据库，不使用批量更新
	if err := IncreaseUserQuota(userId, quotaAwarded, true); err != nil {
		// 如果增加额度失败，需要回滚签到记录
		DB.Delete(checkin)
		return nil, errors.New("签到失败：更新额度出错")
	}

	return checkin, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]interface{}, error) {
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
	hasCheckedToday, _ := HasCheckedInToday(userId)

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)

	return map[string]interface{}{
		"total_quota":      totalQuota,      // 所有时间累计获得的额度
		"total_checkins":   totalCheckins,   // 所有时间累计签到次数
		"checkin_count":    len(records),    // 本月签到次数
		"checked_in_today": hasCheckedToday, // 今天是否已签到
		"records":          checkinRecords,  // 本月签到记录详情（不含id和user_id）
	}, nil
}

type UserCheckinSum struct {
	UserId   int
	Username string
	Total    int
}

// GetCheckinQuotaSumByUsers 按用户分组统计未清除的签到额度
func GetCheckinQuotaSumByUsers(startDate, endDate string, userIds []int) ([]UserCheckinSum, error) {
	var results []struct {
		UserId int `gorm:"column:user_id"`
		Total  int `gorm:"column:total"`
	}
	query := DB.Model(&Checkin{}).
		Select("user_id, SUM(quota_awarded) as total").
		Where("checkin_date >= ? AND checkin_date <= ? AND cleared = ?", startDate, endDate, false).
		Group("user_id")
	if len(userIds) > 0 {
		query = query.Where("user_id IN ?", userIds)
	}
	if err := query.Find(&results).Error; err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	ids := make([]int, len(results))
	for i, r := range results {
		ids[i] = r.UserId
	}
	var users []User
	DB.Unscoped().Where("id IN ?", ids).Select("id, username").Find(&users)
	usernameMap := make(map[int]string, len(users))
	for _, u := range users {
		usernameMap[u.Id] = u.Username
	}

	sums := make([]UserCheckinSum, len(results))
	for i, r := range results {
		sums[i] = UserCheckinSum{
			UserId:   r.UserId,
			Username: usernameMap[r.UserId],
			Total:    r.Total,
		}
	}
	return sums, nil
}

type ClearCheckinPreviewItem struct {
	UserId        int    `json:"user_id"`
	Username      string `json:"username"`
	CheckinQuota  int    `json:"checkin_quota"`
	CurrentQuota  int    `json:"current_quota"`
	ActualClear   int    `json:"actual_clear"`
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
		quota, err := GetUserQuota(s.UserId, true)
		if err != nil {
			continue
		}
		actualClear := s.Total
		if actualClear > quota {
			actualClear = quota
		}
		items = append(items, ClearCheckinPreviewItem{
			UserId:       s.UserId,
			Username:     s.Username,
			CheckinQuota: s.Total,
			CurrentQuota: quota,
			ActualClear:  actualClear,
		})
	}
	return items, nil
}

// BatchClearCheckinQuota 批量清除签到额度
func BatchClearCheckinQuota(startDate, endDate string, userIds []int, operatorName string) (affectedUsers int, totalCleared int, err error) {
	sums, err := GetCheckinQuotaSumByUsers(startDate, endDate, userIds)
	if err != nil {
		return 0, 0, err
	}
	if len(sums) == 0 {
		return 0, 0, nil
	}

	if common.UsingSQLite {
		return batchClearCheckinWithoutTransaction(sums, startDate, endDate, userIds, operatorName)
	}
	return batchClearCheckinWithTransaction(sums, startDate, endDate, userIds, operatorName)
}

type clearOp struct {
	userId    int
	deduction int
}

func batchClearCheckinWithTransaction(sums []UserCheckinSum, startDate, endDate string, userIds []int, operatorName string) (int, int, error) {
	var ops []clearOp

	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, s := range sums {
			var quota int
			if err := tx.Model(&User{}).Where("id = ?", s.UserId).Select("quota").Scan(&quota).Error; err != nil {
				continue
			}
			actualDeduction := s.Total
			if actualDeduction > quota {
				actualDeduction = quota
			}
			if actualDeduction <= 0 {
				continue
			}

			if err := tx.Model(&User{}).Where("id = ?", s.UserId).
				Update("quota", gorm.Expr("CASE WHEN quota >= ? THEN quota - ? ELSE 0 END", actualDeduction, actualDeduction)).Error; err != nil {
				return err
			}

			ops = append(ops, clearOp{userId: s.UserId, deduction: actualDeduction})
		}

		// 标记签到记录为已清除
		markQuery := tx.Model(&Checkin{}).
			Where("checkin_date >= ? AND checkin_date <= ? AND cleared = ?", startDate, endDate, false)
		if len(userIds) > 0 {
			markQuery = markQuery.Where("user_id IN ?", userIds)
		}
		if err := markQuery.Update("cleared", true).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	// 事务成功后：更新缓存和写日志
	totalCleared := 0
	for _, op := range ops {
		totalCleared += op.deduction
		go func(userId int, delta int64) {
			_ = cacheDecrUserQuota(userId, delta)
		}(op.userId, int64(op.deduction))
		RecordLogWithQuota(op.userId, LogTypeManage,
			fmt.Sprintf("%s 清除了用户 %s 至 %s 期间的签到额度 %s",
				operatorName, startDate, endDate, logger.LogQuota(op.deduction)),
			-op.deduction)
	}

	return len(ops), totalCleared, nil
}

func batchClearCheckinWithoutTransaction(sums []UserCheckinSum, startDate, endDate string, userIds []int, operatorName string) (int, int, error) {
	var ops []clearOp

	for _, s := range sums {
		quota, err := GetUserQuota(s.UserId, true)
		if err != nil {
			continue
		}
		actualDeduction := s.Total
		if actualDeduction > quota {
			actualDeduction = quota
		}
		if actualDeduction <= 0 {
			continue
		}

		if err := DecreaseUserQuota(s.UserId, actualDeduction); err != nil {
			continue
		}

		ops = append(ops, clearOp{userId: s.UserId, deduction: actualDeduction})
	}

	// 标记签到记录为已清除
	markQuery := DB.Model(&Checkin{}).
		Where("checkin_date >= ? AND checkin_date <= ? AND cleared = ?", startDate, endDate, false)
	if len(userIds) > 0 {
		markQuery = markQuery.Where("user_id IN ?", userIds)
	}
	if err := markQuery.Update("cleared", true).Error; err != nil {
		return 0, 0, fmt.Errorf("标记签到记录失败: %w", err)
	}

	// 标记成功后才写日志
	totalCleared := 0
	for _, op := range ops {
		totalCleared += op.deduction
		RecordLogWithQuota(op.userId, LogTypeManage,
			fmt.Sprintf("%s 清除了用户 %s 至 %s 期间的签到额度 %s",
				operatorName, startDate, endDate, logger.LogQuota(op.deduction)),
			-op.deduction)
	}

	return len(ops), totalCleared, nil
}
