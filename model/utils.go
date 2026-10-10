package model

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

// The queue alone is not a pending ledger: batches leave it before SQL runs.
// Keep the cache generation and in-flight state until persistence is confirmed.
// This is process-local, not a distributed ledger for shared Redis deployments.
type batchUserQuotaState struct {
	generation   string
	dirty        bool
	flushing     bool
	hydrating    bool
	revision     uint64
	transactions int
	uncertain    bool
}

var userQuotaCacheMu sync.Mutex
var batchUpdateMu sync.Mutex
var batchUserQuotaStates = make(map[int]*batchUserQuotaState)

func batchUserQuotaPendingLocked(id int) bool {
	_, queued := batchUpdateStores[BatchUpdateTypeUserQuota][id]
	state := batchUserQuotaStates[id]
	return queued || (state != nil && (state.flushing || state.transactions > 0 || state.uncertain))
}

func finishBatchUserQuotaStateLocked(id int) {
	state := batchUserQuotaStates[id]
	if state == nil || state.hydrating || batchUserQuotaPendingLocked(id) {
		return
	}
	if !state.dirty && state.generation != "" && common.RedisEnabled && common.RDB != nil {
		generation, err := common.RDB.HGet(context.Background(), getUserCacheKey(id), "QuotaGeneration").Result()
		state.dirty = err != nil || generation != state.generation
	}
	if state.dirty {
		if err := invalidateUserCache(id); err != nil {
			common.SysError(common.LogText("failed to invalidate dirty batch user quota cache: user=%d error=%v", id, err))
			return
		}
	}
	delete(batchUserQuotaStates, id)
}

func init() {
	for range BatchUpdateTypeCount {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	common.SysLog(common.LogText("batch wallet cache guards are process-local; multiple quota-writing processes sharing Redis are unsupported"))
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

func addNewRecord(type_ int, id int, value int) {
	if type_ == BatchUpdateTypeUserQuota {
		userQuotaCacheMu.Lock()
		defer userQuotaCacheMu.Unlock()
		state := batchUserQuotaStates[id]
		if state == nil {
			state = &batchUserQuotaState{}
			batchUserQuotaStates[id] = state
		}
		// Legacy settlement/refund paths update Redis asynchronously. Do not
		// authorize another spend from that hash before this batch commits.
		state.dirty = true
		state.revision++
	}
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	addBatchUpdateRecordLocked(type_, id, value)
}

// A queued wallet delta must adjust the live cache exactly once, before another
// reservation can run. Its caller must not schedule a second cache callback.
func queueUserQuotaDelta(id, delta int) {
	userQuotaCacheMu.Lock()
	defer userQuotaCacheMu.Unlock()
	generation, generationErr := batchUserQuotaGenerationLocked(id)
	state := batchUserQuotaStates[id]
	if state == nil {
		state = &batchUserQuotaState{generation: generation}
		batchUserQuotaStates[id] = state
	}
	state.revision++
	if generationErr == nil {
		result, err := cacheApplyUserQuotaDeltaAtGeneration(id, int64(delta), generation)
		if err != nil || result != cacheQuotaOK {
			state.dirty = true
			common.SysError(common.LogText("batch user quota cache delta failed; spend blocked until persistence: user=%d delta=%d result=%d error=%v", id, delta, result, err))
		}
	} else {
		state.dirty = true
		common.SysLog(common.LogText("batch user quota cache unavailable; spend blocked until persistence: user=%d delta=%d", id, delta))
	}
	// Cache failure must not discard settlement/refund accounting. Only the
	// next spend is blocked; a confirmed SQL flush allows cache reconstruction.
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	addBatchUpdateRecordLocked(BatchUpdateTypeUserQuota, id, delta)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
}

func addBatchUpdateRecordLocked(type_ int, id int, value int) {
	old, ok := batchUpdateStores[type_][id]
	if !ok {
		batchUpdateStores[type_][id] = value
		return
	}

	sum := old + value
	if (value > 0 && sum < old) || (value < 0 && sum > old) {
		common.SysError(common.LogText("batch update overflow: type=%d id=%d old=%d value=%d", type_, id, old, value))
		if value > 0 {
			sum = math.MaxInt
		} else {
			sum = math.MinInt
		}
	}
	batchUpdateStores[type_][id] = sum
}

func batchUpdate() {
	batchUpdateMu.Lock()
	defer batchUpdateMu.Unlock()
	// check if there's any data to update
	hasData := false
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i]) > 0 {
			hasData = true
			batchUpdateLocks[i].Unlock()
			break
		}
		batchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog(common.LogText("batch update started"))
	stores := make([]map[int]int, BatchUpdateTypeCount)
	for i := range BatchUpdateTypeCount {
		if i == BatchUpdateTypeUserQuota {
			userQuotaCacheMu.Lock()
		}
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
		if i == BatchUpdateTypeUserQuota {
			for id := range stores[i] {
				state := batchUserQuotaStates[id]
				if state == nil {
					state = &batchUserQuotaState{dirty: true}
					batchUserQuotaStates[id] = state
				}
				state.flushing = true
			}
			userQuotaCacheMu.Unlock()
		}
	}

	for i, store := range stores {
		if i == BatchUpdateTypeUserQuota || i == BatchUpdateTypeUsedQuota || i == BatchUpdateTypeRequestCount {
			continue
		}
		for key, value := range store {
			switch i {
			case BatchUpdateTypeTokenQuota:
				err := increaseTokenQuota(key, value)
				if err != nil {
					common.SysLog(common.LogText("failed to batch update token quota: %s", err.Error()))
				}
			case BatchUpdateTypeChannelUsedQuota:
				updateChannelUsedQuota(key, value)
			}
		}
	}

	userQuotaStore := stores[BatchUpdateTypeUserQuota]
	usedQuotaStore := stores[BatchUpdateTypeUsedQuota]
	requestCountStore := stores[BatchUpdateTypeRequestCount]

	userIDs := make(map[int]struct{}, len(userQuotaStore)+len(usedQuotaStore)+len(requestCountStore))
	for key := range userQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range usedQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range requestCountStore {
		userIDs[key] = struct{}{}
	}
	for key := range userIDs {
		// Never retry a user whose earlier commit has an unknown outcome.
		userQuotaCacheMu.Lock()
		state := batchUserQuotaStates[key]
		uncertain := state != nil && state.uncertain
		userQuotaCacheMu.Unlock()
		var err error
		rolledBack := true
		if !uncertain {
			rolledBack, err = persistBatchUserAccounting(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key])
		} else {
			rolledBack = false
			err = errors.New("previous batch user quota commit outcome is unknown")
		}
		userQuotaCacheMu.Lock()
		state = batchUserQuotaStates[key]
		if state != nil {
			state.flushing = false
		}
		if err != nil {
			common.SysError(common.LogText("failed to persist batch user accounting: user=%d quota=%d used=%d requests=%d rolled_back=%t error=%v", key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key], rolledBack, err))
			if state == nil {
				state = &batchUserQuotaState{}
				batchUserQuotaStates[key] = state
			}
			state.dirty = true
			state.uncertain = state.uncertain || !rolledBack
			for _, update := range []struct {
				kind  int
				value int
			}{
				{BatchUpdateTypeUserQuota, userQuotaStore[key]},
				{BatchUpdateTypeUsedQuota, usedQuotaStore[key]},
				{BatchUpdateTypeRequestCount, requestCountStore[key]},
			} {
				batchUpdateLocks[update.kind].Lock()
				addBatchUpdateRecordLocked(update.kind, key, update.value)
				batchUpdateLocks[update.kind].Unlock()
			}
		} else {
			finishBatchUserQuotaStateLocked(key)
		}
		userQuotaCacheMu.Unlock()
	}
	common.SysLog(common.LogText("batch update finished"))
}

// Only a confirmed rollback permits replaying an additive batch. A commit
// error may mean the delta was applied; retain it for reconciliation, not retry.
func persistBatchUserAccounting(id, quota, usedQuota, requestCount int) (bool, error) {
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return true, nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return true, tx.Error
	}
	defer tx.Rollback()
	result := tx.Model(&User{}).Where("id = ?", id).Updates(map[string]any{
		"quota":         gorm.Expr("quota + ?", quota),
		"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
		"request_count": gorm.Expr("request_count + ?", requestCount),
	})
	if result.Error != nil {
		rollbackErr := tx.Rollback().Error
		return rollbackErr == nil, errors.Join(result.Error, rollbackErr)
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	return false, nil
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
