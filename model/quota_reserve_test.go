package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type quotaCommitErrorPool struct {
	gorm.ConnPool
	err error
}

func (pool *quotaCommitErrorPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	var tx gorm.ConnPool
	var err error
	switch beginner := pool.ConnPool.(type) {
	case gorm.TxBeginner:
		tx, err = beginner.BeginTx(ctx, opts)
	case gorm.ConnPoolBeginner:
		tx, err = beginner.BeginTx(ctx, opts)
	default:
		return nil, errors.New("test pool cannot begin transactions")
	}
	if err != nil {
		return nil, err
	}
	return &quotaCommitErrorTx{ConnPool: tx, err: pool.err}, nil
}

type quotaCommitErrorTx struct {
	gorm.ConnPool
	err error
}

func (tx *quotaCommitErrorTx) Commit() error {
	if err := tx.ConnPool.(gorm.TxCommitter).Commit(); err != nil {
		return err
	}
	// Simulate an acknowledgement loss after the database actually committed.
	return tx.err
}

func (tx *quotaCommitErrorTx) Rollback() error {
	return tx.ConnPool.(gorm.TxCommitter).Rollback()
}

func createReserveTestUser(t *testing.T, quota int) User {
	t.Helper()
	user := User{
		Username:    "reserve-user-" + common.GetRandomString(6),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       quota,
		AffCode:     "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func getUserQuotaFromDB(t *testing.T, id int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func getTokenFromDB(t *testing.T, id int) Token {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	userQuotaCacheMu.Lock()
	batchUserQuotaStates = make(map[int]*batchUserQuotaState)
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
	userQuotaCacheMu.Unlock()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		userQuotaCacheMu.Lock()
		batchUserQuotaStates = make(map[int]*batchUserQuotaState)
		for i := range BatchUpdateTypeCount {
			batchUpdateLocks[i].Lock()
			batchUpdateStores[i] = make(map[int]int)
			batchUpdateLocks[i].Unlock()
		}
		userQuotaCacheMu.Unlock()
	})
}

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	token := createReserveTestToken(t, 80)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestRedisBatchReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id), "batch delta is not flushed yet")

	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved, "stale DB balance must not authorize a second spend")
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, cachedUser.Quota)

	token := createReserveTestToken(t, 9)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	batchUpdate()
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestBatchUserQuotaReserveRequiresLiveCache(t *testing.T) {
	for _, failure := range []string{"expired cache", "redis unavailable", "redis disabled"} {
		t.Run(failure, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			user := createReserveTestUser(t, 10)
			require.NoError(t, populateUserCache(user))
			reserved, err := TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			require.True(t, reserved)
			switch failure {
			case "expired cache":
				server.FastForward(time.Duration(userCacheTTLSeconds()+1) * time.Second)
			case "redis unavailable":
				server.Close()
			case "redis disabled":
				common.RedisEnabled = false
			}
			reserved, err = TryReserveUserQuota(user.Id, 3)
			assert.False(t, reserved)
			assert.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
			assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id))
			batchUpdate()
			assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
		})
	}
}

func TestBatchCacheRebuildCannotAuthorizeQueuedOrDetachedDebits(t *testing.T) {
	for _, phase := range []string{"queued", "detached"} {
		t.Run(phase, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			user := createReserveTestUser(t, 10)
			// Cold cache is safe to hydrate when there is no pending delta.
			reserved, err := TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			require.True(t, reserved)
			checkRebuild := func() {
				server.FlushAll()
				require.ErrorIs(t, populateUserCache(user), ErrUserQuotaCacheUnavailable)
				_, err := GetUserCache(user.Id)
				require.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
				reserved, err := TryReserveUserQuota(user.Id, 3)
				assert.False(t, reserved)
				assert.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
			}
			if phase == "queued" {
				checkRebuild()
			} else {
				callback := "test:detached_quota_cache_rebuild"
				require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
						checkRebuild()
					}
				}))
				t.Cleanup(func() { _ = DB.Callback().Update().Remove(callback) })
			}
			batchUpdate()
			assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
			// Even a direct caller holding the old ten-unit snapshot must re-read
			// the committed balance, and the guard must no longer lock the user.
			require.NoError(t, populateUserCache(user))
			cached, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 2, cached.Quota)
			reserved, err = TryReserveUserQuota(user.Id, 3)
			require.NoError(t, err)
			assert.False(t, reserved)
		})
	}
}

func TestBatchUserAccountingRollbackRetryPreservesOriginalAndNewDeltas(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{
		"used_quota": 10, "request_count": 2,
	}).Error)
	reserved, err := TryReserveUserQuota(user.Id, 40)
	require.NoError(t, err)
	require.True(t, reserved)
	queueUserQuotaDelta(user.Id, 5)
	addNewRecord(BatchUpdateTypeUsedQuota, user.Id, 15)
	addNewRecord(BatchUpdateTypeRequestCount, user.Id, 1)
	forcedErr := errors.New("forced batch update rollback")
	callback := "test:batch_user_accounting_rollback"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			queueUserQuotaDelta(user.Id, 3)
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callback) })
	batchUpdate()
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
	require.ErrorIs(t, populateUserCache(user), ErrUserQuotaCacheUnavailable)
	require.NoError(t, DB.Callback().Update().Remove(callback))
	batchUpdate()
	batchUpdate()
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 68, got.Quota)
	assert.Equal(t, 25, got.UsedQuota)
	assert.Equal(t, 3, got.RequestCount)
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 68, cached.Quota)
}

func TestBatchSettlementAndRefundAllowContinuousReservations(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuota(user.Id, 20)
	require.NoError(t, err)
	require.True(t, reserved)
	require.NoError(t, DecreaseUserQuota(user.Id, 5, false))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err, "settlement must not block a healthy cache until flush")
	assert.Equal(t, 75, cached.Quota)
	require.NoError(t, IncreaseUserQuota(user.Id, 10, false))
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err, "refund must not block a healthy cache until flush")
	assert.Equal(t, 85, cached.Quota)
	reserved, err = TryReserveUserQuota(user.Id, 80)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
	batchUpdate()
	batchUpdate()
	assert.Equal(t, 5, getUserQuotaFromDB(t, user.Id), "each queued delta persists once")
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 5, cached.Quota)
}

func TestBatchZeroNetAndLateCommittedCreditDoNotChangeBalanceTwice(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DecreaseUserQuota(user.Id, 4, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 4, false))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10, cached.Quota)
	batchUpdate()
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10, cached.Quota)
	// A committed credit's callback can arrive after its balance was hydrated.
	require.NoError(t, increaseUserQuota(user.Id, 4))
	server.FlushAll()
	require.NoError(t, populateUserCache(user))
	require.NoError(t, cacheIncrUserQuota(user.Id, 4))
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 14, cached.Quota)
	assert.Equal(t, 14, getUserQuotaFromDB(t, user.Id))
}

func TestBatchHydrationRejectsSnapshotWhenDeltaFlushesDuringRead(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	callback := "test:batch_flush_during_cache_hydration"
	reads := 0
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "User" {
			return
		}
		reads++
		if reads == 2 {
			// The second read is populateUserCache's fresh snapshot. The
			// queue empties before it returns, but that snapshot is now stale.
			queueUserQuotaDelta(user.Id, -4)
			batchUpdate()
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Query().Remove(callback) })
	_, err := GetUserCache(user.Id)
	require.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
	require.NoError(t, DB.Callback().Query().Remove(callback))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err, "rejected hydration must recover on the next fresh read")
	assert.Equal(t, 6, cached.Quota)
	reserved, err := TryReserveUserQuota(user.Id, 7)
	require.NoError(t, err)
	assert.False(t, reserved)
}

func TestBatchCacheGenerationReplacementIsRejectedAndHealsAfterFlush(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, reserved)
	// A foreign writer bypassed this process's pending guard after Redis loss.
	server.FlushAll()
	require.NoError(t, writeUserCache(user.ToBaseUser(), true))
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(user.Id), "QuotaGeneration", common.GetUUID()).Err())
	reserved, err = TryReserveUserQuota(user.Id, 3)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
	batchUpdate()
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, cached.Quota)
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
}

func TestSynchronousReserveRejectsStaleCacheAfterDatabaseFallback(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	// A Redis-outage debit committed without updating the surviving cache.
	reserved, err := reserveUserQuotaDB(user.Id, 8)
	require.NoError(t, err)
	require.True(t, reserved)
	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	quota, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	assert.Equal(t, 10, quota, "the rejected cache reservation is compensated")
}

func createBalancePurchaseTestPlan(t *testing.T, price float64) *SubscriptionPlan {
	t.Helper()
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	plan := &SubscriptionPlan{
		Title:         "Balance purchase regression",
		PriceAmount:   price,
		Enabled:       true,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	return plan
}

func TestBalancePurchaseUsesLiveQuotaWithoutDoubleDebit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		redis        bool
		batch        bool
		pending      int
		interleaved  int
		price        float64
		charged      int
		cacheFailure string
		wantErr      error
	}{
		{name: "database", price: 6, charged: 6},
		{name: "synchronous cold cache", redis: true, price: 6, charged: 6},
		{name: "synchronous redis failure", redis: true, price: 6, charged: 6, cacheFailure: "unavailable"},
		{name: "batch pending affordable", redis: true, batch: true, pending: 4, price: 6, charged: 6},
		{name: "relay reserves during purchase", redis: true, batch: true, interleaved: 4, price: 6, charged: 6},
		{name: "batch pending insufficient", redis: true, batch: true, pending: 8, price: 3, wantErr: common.NewMessage("Insufficient balance")},
		{name: "batch cold cache", redis: true, batch: true, price: 6, cacheFailure: "cold", wantErr: ErrUserQuotaCacheUnavailable},
		{name: "batch redis failure with pending debit", redis: true, batch: true, pending: 8, price: 3, cacheFailure: "unavailable", wantErr: ErrUserQuotaCacheUnavailable},
		{name: "batch redis disabled", batch: true, price: 6, wantErr: ErrUserQuotaCacheUnavailable},
		{name: "free purchase", batch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			common.RedisEnabled = tc.redis
			common.BatchUpdateEnabled = tc.batch
			user := createReserveTestUser(t, 10)
			plan := createBalancePurchaseTestPlan(t, tc.price)
			if tc.batch && tc.redis && tc.cacheFailure != "cold" {
				require.NoError(t, populateUserCache(user))
			}
			if tc.pending > 0 {
				reserved, err := TryReserveUserQuota(user.Id, tc.pending)
				require.NoError(t, err)
				require.True(t, reserved)
			}
			if tc.cacheFailure == "unavailable" {
				server.Close()
			}
			if tc.interleaved > 0 {
				callback := "test:relay_reserve_during_purchase"
				require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "UserSubscription" {
						return
					}
					// The purchase has reserved six, but has not committed its DB
					// debit. A competing relay can spend only the remaining four.
					reserved, err := TryReserveUserQuota(user.Id, tc.interleaved+1)
					require.NoError(t, err)
					assert.False(t, reserved)
					reserved, err = TryReserveUserQuota(user.Id, tc.interleaved)
					require.NoError(t, err)
					require.True(t, reserved)
				}))
				t.Cleanup(func() { _ = DB.Callback().Create().Remove(callback) })
			}
			err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
			if tc.wantErr != nil {
				require.ErrorContains(t, err, tc.wantErr.Error())
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 10-tc.charged, getUserQuotaFromDB(t, user.Id))
			if tc.redis && tc.cacheFailure == "" {
				cached, err := cacheGetUserBase(user.Id)
				require.NoError(t, err)
				assert.Equal(t, 10-tc.pending-tc.interleaved-tc.charged, cached.Quota)
			}
			batchUpdate()
			assert.Equal(t, 10-tc.pending-tc.interleaved-tc.charged, getUserQuotaFromDB(t, user.Id))
			var orders, subscriptions int64
			require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ?", user.Id).Count(&orders).Error)
			require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptions).Error)
			wantCount := int64(1)
			if tc.wantErr != nil {
				wantCount = 0
			}
			assert.Equal(t, wantCount, orders)
			assert.Equal(t, wantCount, subscriptions)
		})
	}
}

func TestBalancePurchaseRollbackCompensatesOnlyItsReservation(t *testing.T) {
	for _, cacheFailure := range []string{"none", "replacement hash", "redis unavailable"} {
		t.Run(cacheFailure, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			user := createReserveTestUser(t, 10)
			plan := createBalancePurchaseTestPlan(t, 6)
			require.NoError(t, populateUserCache(user))
			reserved, err := TryReserveUserQuota(user.Id, 2)
			require.NoError(t, err)
			require.True(t, reserved)

			forcedErr := errors.New("forced subscription order failure")
			callback := "test:balance_purchase_rollback"
			require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "SubscriptionOrder" {
					return
				}
				switch cacheFailure {
				case "replacement hash":
					require.NoError(t, invalidateUserCache(user.Id))
					replacement := user
					replacement.Quota = 8
					// Deliberately bypass the production rebuild guard to check
					// that a foreign/replacement hash cannot receive this refund.
					require.NoError(t, writeUserCache(replacement.ToBaseUser(), true))
				case "redis unavailable":
					server.Close()
				}
				tx.AddError(forcedErr)
			}))
			t.Cleanup(func() { _ = DB.Callback().Create().Remove(callback) })
			err = PurchaseSubscriptionWithBalance(user.Id, plan.Id)
			require.ErrorIs(t, err, forcedErr)
			if cacheFailure != "none" {
				assert.ErrorIs(t, err, ErrUserQuotaCacheCompensation)
			}
			assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id))
			if cacheFailure == "none" {
				cached, err := cacheGetUserBase(user.Id)
				require.NoError(t, err)
				assert.Equal(t, 8, cached.Quota, "rollback must not erase another debit")
			}
			var updated User
			require.NoError(t, DB.First(&updated, user.Id).Error)
			assert.Equal(t, "default", updated.Group)
			var count int64
			require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ?", user.Id).Count(&count).Error)
			assert.Zero(t, count)
			batchUpdate()
			assert.Equal(t, 8, getUserQuotaFromDB(t, user.Id))
		})
	}
}

func TestUnknownQuotaCommitDoesNotRefundReplayOrAuthorizeAnotherSpend(t *testing.T) {
	for _, operation := range []string{"purchase", "synchronous purchase", "batch"} {
		t.Run(operation, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = operation != "synchronous purchase"
			user := createReserveTestUser(t, 10)
			plan := createBalancePurchaseTestPlan(t, 6)
			require.NoError(t, populateUserCache(user))
			if operation == "batch" {
				reserved, err := TryReserveUserQuota(user.Id, 6)
				require.NoError(t, err)
				require.True(t, reserved)
				addNewRecord(BatchUpdateTypeUsedQuota, user.Id, 6)
				addNewRecord(BatchUpdateTypeRequestCount, user.Id, 1)
			}
			originalDB := DB
			t.Cleanup(func() { DB = originalDB })
			forcedErr := errors.New("commit acknowledgement lost")
			DB = DB.Session(&gorm.Session{NewDB: true, Initialized: true})
			DB.Statement.ConnPool = &quotaCommitErrorPool{ConnPool: originalDB.Statement.ConnPool, err: forcedErr}
			if operation != "batch" {
				err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
				require.ErrorIs(t, err, forcedErr)
			} else {
				batchUpdate()
			}
			DB = originalDB
			assert.Equal(t, 4, getUserQuotaFromDB(t, user.Id))
			quota, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
			require.NoError(t, err)
			assert.Equal(t, 4, quota, "unknown commit must not credit the reservation")
			reserved, err := TryReserveUserQuota(user.Id, 1)
			assert.False(t, reserved)
			assert.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
			err = PurchaseSubscriptionWithBalance(user.Id, plan.Id)
			assert.ErrorIs(t, err, ErrUserQuotaCacheUnavailable)
			batchUpdate()
			assert.Equal(t, 4, getUserQuotaFromDB(t, user.Id), "unknown batch must not replay")
			var count int64
			require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ?", user.Id).Count(&count).Error)
			if operation != "batch" {
				assert.EqualValues(t, 1, count)
			} else {
				assert.Zero(t, count)
				var got User
				require.NoError(t, DB.First(&got, user.Id).Error)
				assert.Equal(t, 6, got.UsedQuota)
				assert.Equal(t, 1, got.RequestCount)
			}
		})
	}
}

func TestTransactionQuotaCompensationIsIdempotent(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	var reservation userQuotaTransactionReservation
	forcedErr := errors.New("forced transaction rollback")
	err := DB.Transaction(func(tx *gorm.DB) error {
		reserved, err := reservation.reserve(tx, &user, 6)
		require.NoError(t, err)
		require.True(t, reserved)
		return forcedErr
	})
	require.ErrorIs(t, err, forcedErr)
	for range 2 {
		require.NoError(t, reservation.complete(false))
	}
	assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id))
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10, cached.Quota)
}

func TestTransactionReservePreservesMaximumWalletQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, common.MaxWalletQuota)
	require.NoError(t, populateUserCache(user))
	var reservation userQuotaTransactionReservation
	err := DB.Transaction(func(tx *gorm.DB) error {
		reserved, err := reservation.reserve(tx, &user, common.MaxWalletQuota-1)
		require.NoError(t, err)
		require.True(t, reserved)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, reservation.complete(true))
	assert.Equal(t, 1, getUserQuotaFromDB(t, user.Id))
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, cached.Quota)
}

func TestSubscriptionRefundMarkerFailureRollsBackQuotaAndRetryRefundsOnce(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}))
	requestID := "subscription-refund-" + common.GetUUID()
	t.Cleanup(func() { DB.Where("request_id = ?", requestID).Delete(&SubscriptionPreConsumeRecord{}) })
	sub := UserSubscription{UserId: 1, PlanId: 1, AmountTotal: 100, AmountUsed: 60, Status: "active"}
	require.NoError(t, DB.Create(&sub).Error)
	record := SubscriptionPreConsumeRecord{
		RequestId: requestID, UserId: 1, UserSubscriptionId: sub.Id, PreConsumed: 20, Status: "consumed",
	}
	require.NoError(t, DB.Create(&record).Error)
	forcedErr := errors.New("forced refund marker failure")
	callback := "test:subscription_refund_marker"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "SubscriptionPreConsumeRecord" {
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callback) })
	require.ErrorIs(t, RefundSubscriptionPreConsume(requestID), forcedErr)
	var reloaded UserSubscription
	require.NoError(t, DB.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 60, reloaded.AmountUsed)
	require.NoError(t, DB.First(&record, record.Id).Error)
	assert.Equal(t, "consumed", record.Status)
	require.NoError(t, DB.Callback().Update().Remove(callback))
	for range 2 {
		require.NoError(t, RefundSubscriptionPreConsume(requestID))
	}
	require.NoError(t, DB.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 40, reloaded.AmountUsed)
	require.NoError(t, DB.First(&record, record.Id).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestBatchUpdateAccumulatesTwoMaximumRequestCharges(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, common.MaxQuota*2+100)
	require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))
	require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))

	batchUpdate()
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestBatchUpdateAccumulatorSaturatesOverflow(t *testing.T) {
	resetBatchUpdateTestState(t)

	addNewRecord(BatchUpdateTypeUserQuota, 1, math.MaxInt)
	addNewRecord(BatchUpdateTypeUserQuota, 1, 1)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MaxInt, batchUpdateStores[BatchUpdateTypeUserQuota][1])
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()

	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	addNewRecord(BatchUpdateTypeUserQuota, 1, math.MinInt)
	addNewRecord(BatchUpdateTypeUserQuota, 1, -1)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MinInt, batchUpdateStores[BatchUpdateTypeUserQuota][1])
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
}

func TestReserveFallsBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 20)
	require.NoError(t, populateUserCache(user))
	server.Close()

	// Redis 故障时降级为数据库条件更新：服务保持可用且不会超扣。
	reserved, err := TryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 16)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
}

func TestSynchronousReserveCompensatesCacheWhenPersistenceFails(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DB.Delete(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 6)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cached, cacheErr := cacheGetUserBase(user.Id)
	require.NoError(t, cacheErr)
	assert.Equal(t, 10, cached.Quota)

	token := createReserveTestToken(t, 12)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, DB.Delete(&token).Error)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cachedToken, cacheErr := cacheGetTokenByKey(token.Key)
	require.NoError(t, cacheErr)
	assert.Equal(t, 12, cachedToken.RemainQuota)
	assert.Zero(t, cachedToken.UsedQuota)
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)

	// 已存在的哈希只刷新 TTL：数据库快照不得覆盖已被原子预扣的余额。
	code, err := cacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)

	// 变更期间：fence 删除缓存并拦截并发读者手中的过期快照。
	require.NoError(t, invalidateTokenCacheForMutation(token.Key))
	code, err = cacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// fence 过期后可重新从数据库水合。
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}
