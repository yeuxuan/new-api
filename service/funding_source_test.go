package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupWalletFundingTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.BonusQuotaGrant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	model.DB = db
	db.Create(&model.User{Id: 1, Quota: 5000})
}

func TestWalletFundingBonusFirst(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceCheckin, "2026-06-01", 3000, 0)
	if err != nil {
		t.Fatalf("create bonus: %v", err)
	}

	w := &WalletFunding{userId: 1, modelName: "gpt-4"}
	if err := w.PreConsume(4000); err != nil {
		t.Fatalf("preconsume: %v", err)
	}
	if w.bonusConsumed != 3000 || w.consumed != 1000 {
		t.Fatalf("bonus=%d wallet=%d", w.bonusConsumed, w.consumed)
	}

	if err := w.Refund(); err != nil {
		t.Fatalf("refund: %v", err)
	}

	bonus, _ := model.GetUserBonusQuotaTotal(1)
	quota, _ := model.GetUserQuota(1, true)
	if bonus != 3000 || quota != 5000 {
		t.Fatalf("after refund bonus=%d quota=%d", bonus, quota)
	}
}
