package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupBonusQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&BonusQuotaGrant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestCreateAndConsumeBonusQuotaFIFO(t *testing.T) {
	setupBonusQuotaTestDB(t)

	now := time.Now().Unix()
	_, err := CreateBonusQuotaGrant(1, BonusQuotaSourceCheckin, "2026-06-01", 1000, now+86400)
	if err != nil {
		t.Fatalf("create grant1: %v", err)
	}
	_, err = CreateBonusQuotaGrant(1, BonusQuotaSourceCheckin, "2026-06-02", 2000, now+172800)
	if err != nil {
		t.Fatalf("create grant2: %v", err)
	}

	total, err := GetAvailableBonusQuota(1, "gpt-4")
	if err != nil || total != 3000 {
		t.Fatalf("available = %d, err = %v", total, err)
	}

	deductions, consumed, err := ConsumeBonusQuota(1, "gpt-4", 1500)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed != 1500 {
		t.Fatalf("consumed = %d", consumed)
	}
	if len(deductions) == 0 {
		t.Fatal("expected deductions")
	}

	remaining, err := GetAvailableBonusQuota(1, "gpt-4")
	if err != nil || remaining != 1500 {
		t.Fatalf("remaining = %d, err = %v", remaining, err)
	}

	if err := RefundBonusQuota(deductions); err != nil {
		t.Fatalf("refund: %v", err)
	}
	remaining, err = GetAvailableBonusQuota(1, "gpt-4")
	if err != nil || remaining != 3000 {
		t.Fatalf("after refund remaining = %d, err = %v", remaining, err)
	}
}

func TestBonusQuotaModelRestriction(t *testing.T) {
	setupBonusQuotaTestDB(t)

	setting := operation_setting.GetBonusQuotaSetting()
	oldModels := setting.AllowedModels
	setting.AllowedModels = "gpt-4"
	defer func() { setting.AllowedModels = oldModels }()

	_, err := CreateBonusQuotaGrant(2, BonusQuotaSourceEmailBind, "", 5000, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	allowed, err := GetAvailableBonusQuota(2, "gpt-4")
	if err != nil || allowed != 5000 {
		t.Fatalf("allowed model quota = %d, err = %v", allowed, err)
	}

	denied, err := GetAvailableBonusQuota(2, "claude-3")
	if err != nil || denied != 0 {
		t.Fatalf("denied model quota = %d, err = %v", denied, err)
	}
}

func TestExpireDueBonusQuotas(t *testing.T) {
	setupBonusQuotaTestDB(t)

	past := time.Now().Unix() - 3600
	_, err := CreateBonusQuotaGrant(3, BonusQuotaSourceCheckin, "2026-06-01", 1000, past)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	n, err := ExpireDueBonusQuotas(100)
	if err != nil || n != 1 {
		t.Fatalf("expired = %d, err = %v", n, err)
	}

	total, err := GetUserBonusQuotaTotal(3)
	if err != nil || total != 0 {
		t.Fatalf("total after expire = %d, err = %v", total, err)
	}
}

func TestHasEmailBindBonusGrant(t *testing.T) {
	setupBonusQuotaTestDB(t)

	ok, err := HasEmailBindBonusGrant(4)
	if err != nil || ok {
		t.Fatalf("expected false, got %v err=%v", ok, err)
	}

	_, err = CreateBonusQuotaGrant(4, BonusQuotaSourceEmailBind, "", 1000, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err = HasEmailBindBonusGrant(4)
	if err != nil || !ok {
		t.Fatalf("expected true, got %v err=%v", ok, err)
	}
}

func TestEnsureBonusQuotaGrantUniqueIndex(t *testing.T) {
	setupBonusQuotaTestDB(t)

	if err := MigrateBonusQuotaGrantUniqueIndex(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := EnsureBonusQuotaGrantUniqueIndex(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !DB.Migrator().HasIndex(&BonusQuotaGrant{}, "idx_bonus_quota_user_source_ref") {
		t.Fatal("expected per-user unique index")
	}
}

func TestBonusQuotaGrantUniquePerUser(t *testing.T) {
	setupBonusQuotaTestDB(t)

	date := "2026-06-01"
	_, err := CreateBonusQuotaGrant(1, BonusQuotaSourceCheckin, date, 1000, 0)
	if err != nil {
		t.Fatalf("user1 checkin grant: %v", err)
	}
	_, err = CreateBonusQuotaGrant(2, BonusQuotaSourceCheckin, date, 2000, 0)
	if err != nil {
		t.Fatalf("user2 same-day checkin grant: %v", err)
	}
	_, err = CreateBonusQuotaGrant(1, BonusQuotaSourceEmailBind, "", 500, 0)
	if err != nil {
		t.Fatalf("user1 email bind grant: %v", err)
	}
	_, err = CreateBonusQuotaGrant(2, BonusQuotaSourceEmailBind, "", 500, 0)
	if err != nil {
		t.Fatalf("user2 email bind grant: %v", err)
	}
}

func TestBonusQuotaPartialConsume(t *testing.T) {
	setupBonusQuotaTestDB(t)

	_, err := CreateBonusQuotaGrant(5, BonusQuotaSourceCheckin, "2026-06-01", 800, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, consumed, err := ConsumeBonusQuota(5, "gpt-4", 2000)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed != 800 {
		t.Fatalf("expected partial consume 800, got %d", consumed)
	}

	remaining, err := GetUserBonusQuotaTotal(5)
	if err != nil || remaining != 0 {
		t.Fatalf("remaining = %d, err = %v", remaining, err)
	}
}
