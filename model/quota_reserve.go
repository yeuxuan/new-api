package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

var ErrUserQuotaCacheUnavailable = common.NewMessage("Wallet balance is temporarily unavailable; please try again later")

var ErrUserQuotaCacheCompensation = common.NewMessage("Wallet reservation refund failed; please contact the administrator")

var errUserQuotaInsufficient = errors.New("insufficient user quota")

const userQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
if ARGV[5] ~= '' and redis.call('HGET', KEYS[1], 'QuotaGeneration') ~= ARGV[5] then
  return -1
end
if ARGV[4] ~= '' and redis.call('HEXISTS', KEYS[1], ARGV[4]) == 1 then
  return 1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', '-' .. ARGV[1])
if ARGV[4] ~= '' then
  redis.call('HSET', KEYS[1], ARGV[4], ARGV[1])
end
return 1`

const userQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
if ARGV[4] ~= '' and redis.call('HGET', KEYS[1], 'QuotaGeneration') ~= ARGV[4] then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', ARGV[1])
return 1`

const tokenQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
if remain == nil or remain < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', -tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func cacheTryReserveUserQuota(userID int, amount int64) (cacheQuotaResult, error) {
	return cacheReserveUserQuotaAtGeneration(userID, amount, "", "")
}

func cacheReserveUserQuotaAtGeneration(userID int, amount int64, receipt, generation string) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{getUserCacheKey(userID)}, amount, userID, userCacheSchemaVersion, receipt, generation).Int()
	return quotaResultFromLua(result, err)
}

func batchUserQuotaGenerationLocked(id int) (string, error) {
	state := batchUserQuotaStates[id]
	if state != nil && (state.dirty || state.hydrating || state.uncertain) {
		return "", ErrUserQuotaCacheUnavailable
	}
	if !common.RedisEnabled || common.RDB == nil {
		return "", ErrUserQuotaCacheUnavailable
	}
	generation, err := common.RDB.HGet(context.Background(), getUserCacheKey(id), "QuotaGeneration").Result()
	if err != nil || generation == "" || (state != nil && state.generation != "" && state.generation != generation) {
		return "", ErrUserQuotaCacheUnavailable
	}
	return generation, nil
}

// A transaction reservation changes only Redis. Its database debit belongs to
// the caller's transaction, never to the batch queue. The receipt lives in the
// same hash so compensation cannot credit a replacement database snapshot.
type userQuotaTransactionReservation struct {
	userID   int
	quota    int
	receipt  string
	fallback bool
	batch    bool
}

func (r *userQuotaTransactionReservation) reserve(tx *gorm.DB, user *User, quota int) (bool, error) {
	r.userID, r.quota = user.Id, quota
	userQuotaCacheMu.Lock()
	state := batchUserQuotaStates[user.Id]
	uncertain := state != nil && state.uncertain
	userQuotaCacheMu.Unlock()
	if uncertain {
		return false, ErrUserQuotaCacheUnavailable
	}
	if quota == 0 {
		return true, nil
	}
	generation := ""
	if common.BatchUpdateEnabled {
		userQuotaCacheMu.Lock()
		defer userQuotaCacheMu.Unlock()
		var err error
		generation, err = batchUserQuotaGenerationLocked(user.Id)
		if err != nil {
			return false, err
		}
		state := batchUserQuotaStates[user.Id]
		if state == nil {
			state = &batchUserQuotaState{generation: generation}
			batchUserQuotaStates[user.Id] = state
		}
		state.transactions++
		r.batch = true
	}
	if common.RedisEnabled {
		r.receipt = "QuotaReservation:" + common.GetUUID()
		result, err := cacheReserveUserQuotaAtGeneration(user.Id, int64(quota), r.receipt, generation)
		// Only synchronous mode can hydrate from the locked database snapshot.
		// Batch snapshots may omit queued or already-detached, in-flight debits.
		if err == nil && result == cacheQuotaMiss && !common.BatchUpdateEnabled {
			if hydrateErr := populateUserCache(*user); hydrateErr == nil {
				result, err = cacheReserveUserQuotaAtGeneration(user.Id, int64(quota), r.receipt, generation)
			}
		}
		if err == nil && result == cacheQuotaInsufficient {
			r.receipt = ""
			return false, nil
		}
		if err != nil || result != cacheQuotaOK {
			if err == nil {
				r.receipt = ""
			}
			if common.BatchUpdateEnabled {
				return false, ErrUserQuotaCacheUnavailable
			}
			// Redis may have applied a timed-out reservation. Do not debit it
			// again; the conditional database update remains authoritative.
			r.fallback = true
		}
	} else if common.BatchUpdateEnabled {
		return false, ErrUserQuotaCacheUnavailable
	}
	result := tx.Model(&User{}).Where("id = ? AND quota >= ?", user.Id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func (r *userQuotaTransactionReservation) complete(committed bool) (completeErr error) {
	if r.batch {
		userQuotaCacheMu.Lock()
		defer userQuotaCacheMu.Unlock()
		defer func() {
			state := batchUserQuotaStates[r.userID]
			state.transactions--
			state.dirty = state.dirty || completeErr != nil
			finishBatchUserQuotaStateLocked(r.userID)
			r.batch = false
		}()
	}
	if r.quota == 0 {
		return nil
	}
	if committed && r.fallback {
		// No queued purchase delta exists. Synchronous quota reserves also use
		// a conditional database debit, so a cache miss safely heals from DB.
		return invalidateUserCache(r.userID)
	}
	if r.receipt == "" {
		return nil
	}
	if committed {
		const script = `
if redis.call('HGET', KEYS[1], ARGV[1]) ~= ARGV[2] then
  return 0
end
redis.call('HDEL', KEYS[1], ARGV[1])
return 1`
		result, err := common.RDB.Eval(context.Background(), script,
			[]string{getUserCacheKey(r.userID)}, r.receipt, r.quota).Int()
		if err != nil || result != 1 {
			return fmt.Errorf("%w: committed reservation user=%d result=%d error=%v", ErrUserQuotaCacheUnavailable, r.userID, result, err)
		}
		return nil
	}
	const script = `
local reserved = redis.call('HGET', KEYS[1], ARGV[4])
if reserved == 'refunded' then
  return 1
end
if reserved ~= ARGV[1]
  or tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', ARGV[1])
redis.call('HSET', KEYS[1], ARGV[4], 'refunded')
return 1`
	result, err := common.RDB.Eval(context.Background(), script,
		[]string{getUserCacheKey(r.userID)}, r.quota, r.userID, userCacheSchemaVersion, r.receipt).Int()
	if err != nil || result != 1 {
		return fmt.Errorf("%w: user=%d result=%d error=%v", ErrUserQuotaCacheCompensation, r.userID, result, err)
	}
	return nil
}

// A failed COMMIT/ROLLBACK is not proof that the debit was undone. Quarantine
// the wallet for reconciliation instead of issuing an unsafe credit.
func (r *userQuotaTransactionReservation) quarantine() {
	if r.userID > 0 {
		userQuotaCacheMu.Lock()
		state := batchUserQuotaStates[r.userID]
		if state == nil {
			state = &batchUserQuotaState{}
			batchUserQuotaStates[r.userID] = state
		}
		if r.batch {
			state.transactions--
		}
		state.uncertain = true
		state.dirty = true
		r.batch = false
		userQuotaCacheMu.Unlock()
	}
	common.SysError(common.LogText("wallet transaction outcome unknown; reconciliation required before retry: user=%d quota=%d receipt=%s", r.userID, r.quota, r.receipt))
}

func cacheApplyUserQuotaDelta(userID int, delta int64) (cacheQuotaResult, error) {
	return cacheApplyUserQuotaDeltaAtGeneration(userID, delta, "")
}

func cacheApplyUserQuotaDeltaAtGeneration(userID int, delta int64, generation string) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{getUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion, generation).Int()
	return quotaResultFromLua(result, err)
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{getTokenCacheKey(key)}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaDeltaScript,
		[]string{getTokenCacheKey(key)}, delta, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

// persistUserQuotaDelta persists a synchronous-mode cache reservation. Batch
// reservations enqueue under userQuotaCacheMu before releasing the cache lock.
func persistUserQuotaDelta(id int, delta int) error {
	query := DB.Model(&User{}).Where("id = ?", id)
	if delta < 0 {
		query = query.Where("quota >= ?", -delta)
	}
	result := query.Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var count int64
		if err := DB.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 && delta < 0 {
			return errUserQuotaInsufficient
		}
		return gorm.ErrRecordNotFound
	}
	return nil
}

func persistTokenQuotaDelta(id int, delta int) error {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, delta)
		return nil
	}
	result := DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", delta),
			"used_quota":    gorm.Expr("used_quota - ?", delta),
			"accessed_time": common.GetTimestamp(),
		},
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func reserveUserQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func reserveTokenQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// TryReserveUserQuota atomically checks and deducts a user's wallet quota.
// 缓存命中时以缓存余额为准（避免批量模式下过期的数据库余额放大并发超扣）；
// Only synchronous mode can safely degrade to the database on a cache failure.
func TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	userQuotaCacheMu.Lock()
	state := batchUserQuotaStates[id]
	uncertain := state != nil && state.uncertain
	userQuotaCacheMu.Unlock()
	if uncertain {
		return false, ErrUserQuotaCacheUnavailable
	}
	if common.BatchUpdateEnabled {
		if _, err := GetUserCache(id); err != nil {
			return false, err
		}
		userQuotaCacheMu.Lock()
		defer userQuotaCacheMu.Unlock()
		generation, err := batchUserQuotaGenerationLocked(id)
		if err != nil {
			return false, err
		}
		result, err := cacheReserveUserQuotaAtGeneration(id, int64(quota), "", generation)
		if err != nil || result == cacheQuotaMiss {
			return false, ErrUserQuotaCacheUnavailable
		}
		if result == cacheQuotaInsufficient {
			return false, nil
		}
		state := batchUserQuotaStates[id]
		if state == nil {
			state = &batchUserQuotaState{generation: generation}
			batchUserQuotaStates[id] = state
		}
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		addBatchUpdateRecordLocked(BatchUpdateTypeUserQuota, id, -quota)
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
		return true, nil
	}
	if !common.RedisEnabled {
		return reserveUserQuotaDB(id, quota)
	}

	result, err := cacheTryReserveUserQuota(id, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetUserCache(id); hydrateErr == nil {
			result, err = cacheTryReserveUserQuota(id, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog(common.LogText("user quota cache reserve unavailable, falling back to database: %s", err.Error()))
		}
		return reserveUserQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistUserQuotaDelta(id, -quota); err != nil {
		compensated, compensateErr := cacheApplyUserQuotaDelta(id, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(common.LogText("failed to compensate reserved user quota: result=%d error=%v", compensated, compensateErr))
			return false, errors.Join(ErrUserQuotaCacheCompensation, err)
		}
		if errors.Is(err, errUserQuotaInsufficient) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TryReserveTokenQuota atomically checks and deducts a token quota. Unlimited
// tokens skip the balance check but still update remain/used accounting.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, DecreaseTokenQuota(id, key, quota)
	}
	if !common.RedisEnabled {
		return reserveTokenQuotaDB(id, quota)
	}

	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
			result, err = cacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog(common.LogText("token quota cache reserve unavailable, falling back to database: %s", err.Error()))
		}
		return reserveTokenQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistTokenQuotaDelta(id, -quota); err != nil {
		compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(common.LogText("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}
