package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletFundingTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.BonusQuotaGrant{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Quota: 5000}).Error)
}

func TestWalletFundingBonusFirst(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceCheckin, "2026-06-01", 3000, 0)
	require.NoError(t, err)

	w := &WalletFunding{userId: 1, modelName: "gpt-4"}
	require.NoError(t, w.PreConsume(4000))
	assert.Equal(t, 3000, w.bonusConsumed)
	assert.Equal(t, 1000, w.consumed)

	require.NoError(t, w.Refund())

	bonus, err := model.GetUserBonusQuotaTotal(1)
	require.NoError(t, err)
	quota, err := model.GetUserQuota(1, true)
	require.NoError(t, err)
	assert.Equal(t, 3000, bonus)
	assert.Equal(t, 5000, quota)
}

func TestWalletFundingInsufficientQuotaRollsBackBonus(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceCheckin, "2026-06-02", 3000, 0)
	require.NoError(t, err)

	w := &WalletFunding{userId: 1, modelName: "gpt-4"}
	require.ErrorIs(t, w.PreConsume(9000), ErrInsufficientWalletQuota)
	assert.Zero(t, w.bonusConsumed)
	assert.Zero(t, w.consumed)
	assert.Empty(t, w.bonusDeducts)

	bonus, err := model.GetUserBonusQuotaTotal(1)
	require.NoError(t, err)
	quota, err := model.GetUserQuota(1, true)
	require.NoError(t, err)
	assert.Equal(t, 3000, bonus)
	assert.Equal(t, 5000, quota)
}

func TestWalletFundingRefundClearedGrantDoesNotBecomeCash(t *testing.T) {
	setupWalletFundingTestDB(t)

	grant := &model.BonusQuotaGrant{
		UserId:          1,
		Source:          model.BonusQuotaSourceCheckin,
		SourceRef:       "cleared-refund",
		AmountTotal:     3000,
		AmountRemaining: 0,
		Status:          model.BonusQuotaStatusCleared,
	}
	require.NoError(t, model.DB.Create(grant).Error)

	// The wallet part was consumed separately; the cleared grant must not be
	// revived or converted into wallet quota during the same refund.
	w := &WalletFunding{
		userId:        1,
		modelName:     "gpt-4",
		consumed:      1000,
		bonusConsumed: 3000,
		bonusDeducts:  []model.BonusQuotaDeduction{{GrantId: grant.Id, Amount: 3000}},
	}
	require.NoError(t, w.Refund())

	bonus, err := model.GetUserBonusQuotaTotal(1)
	require.NoError(t, err)
	quota, err := model.GetUserQuota(1, true)
	require.NoError(t, err)
	assert.Zero(t, bonus)
	assert.Equal(t, 6000, quota)
	var persisted model.BonusQuotaGrant
	require.NoError(t, model.DB.First(&persisted, grant.Id).Error)
	assert.Equal(t, model.BonusQuotaStatusCleared, persisted.Status)
	assert.Zero(t, persisted.AmountRemaining)
}
