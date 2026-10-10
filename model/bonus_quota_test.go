package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestApplyTaskWalletQuotaDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_FIXED_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_FIXED_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldDB, oldRedis := DB, common.RedisEnabled
			oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, common.RedisEnabled = db, false
			common.SetDatabaseTypes(dialect.name, oldLog)
			setting := operation_setting.GetBonusQuotaSetting()
			oldModels := setting.AllowedModels
			setting.AllowedModels = "allowed-model"
			t.Cleanup(func() {
				DB, common.RedisEnabled = oldDB, oldRedis
				common.SetDatabaseTypes(oldMain, oldLog)
				setting.AllowedModels = oldModels
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Channel{}, &Task{}, &BonusQuotaGrant{}))
			query := "select version()"
			if dialect.name == common.DatabaseTypeSQLite {
				query = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database: %s", version)
			for _, tc := range []struct {
				name, status                  string
				remaining, actual, wantWallet int
				wantBonus, wantDeductions     int
				due, denied, foreignToken     bool
				walletOverflow                bool
			}{
				{name: "live grant full refund", status: BonusQuotaStatusActive, remaining: 40, wantWallet: 140, wantBonus: 100},
				{name: "depleted grant full refund", status: BonusQuotaStatusDepleted, wantWallet: 140, wantBonus: 60},
				{name: "expired grant never becomes cash", status: BonusQuotaStatusExpired, wantWallet: 140},
				{name: "cleared grant never revives or becomes cash", status: BonusQuotaStatusCleared, wantWallet: 140},
				{name: "due depleted grant never revives", status: BonusQuotaStatusDepleted, due: true, wantWallet: 140},
				{name: "partial bonus refund", status: BonusQuotaStatusActive, remaining: 40, actual: 60, wantWallet: 100, wantBonus: 80, wantDeductions: 20},
				{name: "unchanged quota preserves settlement snapshot", status: BonusQuotaStatusActive, remaining: 40, actual: 100, wantWallet: 100, wantBonus: 40, wantDeductions: 60},
				{name: "additional charge uses live bonus", status: BonusQuotaStatusActive, remaining: 40, actual: 130, wantWallet: 100, wantBonus: 10, wantDeductions: 90},
				{name: "raw model restriction preserved", status: BonusQuotaStatusActive, remaining: 40, actual: 130, denied: true, wantWallet: 70, wantBonus: 40, wantDeductions: 60},
				{name: "token ownership failure rolls back all balances", status: BonusQuotaStatusActive, remaining: 40, foreignToken: true},
				{name: "wallet overflow rolls back grant refund", status: BonusQuotaStatusActive, remaining: 40, walletOverflow: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := User{Username: "task_" + common.GetUUID(), Quota: 100, UsedQuota: 100, RequestCount: 1}
					if tc.walletOverflow {
						user.Quota = common.MaxWalletQuota
					}
					require.NoError(t, db.Create(&user).Error)
					t.Cleanup(func() { require.NoError(t, db.Unscoped().Delete(&user).Error) })
					channel := Channel{Name: "wallet_matrix", Key: "test", UsedQuota: 100}
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&channel).Error) })
					token := Token{UserId: user.Id, Key: common.GetUUID(), RemainQuota: 900, UsedQuota: 100}
					if tc.foreignToken {
						token.UserId++
					}
					require.NoError(t, db.Create(&token).Error)
					t.Cleanup(func() { require.NoError(t, db.Unscoped().Delete(&token).Error) })
					grant := BonusQuotaGrant{UserId: user.Id, Source: BonusQuotaSourceCheckin,
						SourceRef: "matrix", AmountTotal: 60 + tc.remaining, AmountRemaining: tc.remaining, Status: tc.status}
					if tc.due {
						grant.ExpiresAt = time.Now().Unix() - 1
					}
					require.NoError(t, db.Create(&grant).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&grant).Error) })
					modelName := "allowed-model"
					if tc.denied {
						modelName = "allowed-model-high"
					}
					task := Task{TaskID: fmt.Sprintf("wallet_%s", common.GetUUID()), UserId: user.Id, ChannelId: channel.Id, Quota: 100,
						Properties: Properties{OriginModelName: modelName},
						PrivateData: TaskPrivateData{BillingSource: "wallet", TokenId: token.Id,
							BonusQuotaDeducts: []BonusQuotaDeduction{{GrantId: grant.Id, Amount: 60}}}}
					require.NoError(t, db.Create(&task).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&task).Error) })
					stale := task
					task.PrivateData.BillingContext = &TaskBillingContext{OriginModelName: modelName, ModelPrice: 1.25}
					applied, err := ApplyTaskWalletQuota(&task, tc.actual)
					if tc.foreignToken || tc.walletOverflow {
						require.Error(t, err)
						assert.False(t, applied)
						var persisted Task
						require.NoError(t, db.First(&persisted, task.ID).Error)
						assert.Equal(t, 100, persisted.Quota)
						assert.Equal(t, stale.PrivateData.BonusQuotaDeducts, persisted.PrivateData.BonusQuotaDeducts)
						assert.Nil(t, persisted.PrivateData.BillingContext)
						var got User
						require.NoError(t, db.First(&got, user.Id).Error)
						assert.Equal(t, user.Quota, got.Quota)
						assert.Equal(t, 100, got.UsedQuota)
						require.NoError(t, db.First(&grant, grant.Id).Error)
						assert.Equal(t, tc.remaining, grant.AmountRemaining)
						assert.Equal(t, tc.status, grant.Status)
						return
					}
					require.NoError(t, err)
					assert.Equal(t, tc.actual != 100, applied)
					applied, err = ApplyTaskWalletQuota(&stale, tc.actual)
					require.NoError(t, err)
					assert.False(t, applied, "stale replay cannot move funds twice")
					var persisted Task
					require.NoError(t, db.First(&persisted, task.ID).Error)
					assert.Equal(t, tc.actual, persisted.Quota)
					require.NotNil(t, persisted.PrivateData.BillingContext)
					assert.Equal(t, modelName, persisted.PrivateData.BillingContext.OriginModelName)
					assert.Equal(t, 1.25, persisted.PrivateData.BillingContext.ModelPrice)
					total := 0
					for _, d := range persisted.PrivateData.BonusQuotaDeducts {
						total += d.Amount
					}
					assert.Equal(t, tc.wantDeductions, total)
					require.NoError(t, db.First(&user, user.Id).Error)
					assert.Equal(t, tc.wantWallet, user.Quota)
					assert.Equal(t, tc.actual, user.UsedQuota)
					assert.Equal(t, 1, user.RequestCount)
					require.NoError(t, db.First(&channel, channel.Id).Error)
					assert.Equal(t, int64(tc.actual), channel.UsedQuota)
					require.NoError(t, db.First(&token, token.Id).Error)
					assert.Equal(t, 1000-tc.actual, token.RemainQuota)
					assert.Equal(t, tc.actual, token.UsedQuota)
					require.NoError(t, db.First(&grant, grant.Id).Error)
					assert.Equal(t, tc.wantBonus, grant.AmountRemaining)
					if tc.status == BonusQuotaStatusCleared || tc.status == BonusQuotaStatusExpired || tc.due {
						assert.Equal(t, tc.status, grant.Status)
						// The synchronous funding caller must obey the same tombstones.
						require.NoError(t, RefundBonusQuota([]BonusQuotaDeduction{{GrantId: grant.Id, Amount: 60}}))
						require.NoError(t, db.First(&grant, grant.Id).Error)
						assert.Zero(t, grant.AmountRemaining)
						assert.Equal(t, tc.status, grant.Status)
					}
				})
			}
		})
	}
}

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
