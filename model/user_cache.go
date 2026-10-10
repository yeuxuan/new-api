package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

const userCacheSchemaVersion = 2

type UserBase struct {
	Id              int    `json:"id"`
	Group           string `json:"group"`
	Email           string `json:"email"`
	Quota           int    `json:"quota"`
	Status          int    `json:"status"`
	Role            int    `json:"role"`
	Username        string `json:"username"`
	Setting         string `json:"setting"`
	AuthVersion     int64  `json:"-"`
	CacheSchema     int    `json:"-"`
	QuotaGeneration string `json:"-"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal setting: %s", err.Error()))
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

func userCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

func populateUserCache(user User) error {
	userQuotaCacheMu.Lock()
	if batchUserQuotaPendingLocked(user.Id) {
		userQuotaCacheMu.Unlock()
		common.SysLog(common.LogText("user quota cache rebuild blocked while batch accounting is pending: user=%d", user.Id))
		return ErrUserQuotaCacheUnavailable
	}
	if !common.RedisEnabled {
		userQuotaCacheMu.Unlock()
		return nil
	}
	if !common.BatchUpdateEnabled {
		defer userQuotaCacheMu.Unlock()
		return writeUserCache(user.ToBaseUser(), true)
	}
	state := batchUserQuotaStates[user.Id]
	if state != nil && state.hydrating {
		userQuotaCacheMu.Unlock()
		return ErrUserQuotaCacheUnavailable
	}
	if state == nil {
		state = &batchUserQuotaState{}
		batchUserQuotaStates[user.Id] = state
	}
	state.hydrating = true
	revision := state.revision
	userQuotaCacheMu.Unlock()
	// Callers may hold a snapshot read before the previous batch committed.
	// Do not hold the cache mutex during DB reads: a purchasing transaction
	// can hold a connection while waiting to reserve from the cache.
	fresh, err := GetUserById(user.Id, false)
	userQuotaCacheMu.Lock()
	defer userQuotaCacheMu.Unlock()
	state.hydrating = false
	defer finishBatchUserQuotaStateLocked(user.Id)
	if err != nil {
		return err
	}
	if state.revision != revision || batchUserQuotaPendingLocked(user.Id) {
		return ErrUserQuotaCacheUnavailable
	}
	// Another caller may have filled a healthy hash while this DB read ran.
	// Preserve its live quota, including reservations from other processes.
	var live UserBase
	if !state.dirty && common.RedisHGetObj(getUserCacheKey(user.Id), &live) == nil &&
		live.Id == user.Id && live.CacheSchema == userCacheSchemaVersion && live.AuthVersion > 0 &&
		live.QuotaGeneration != "" && (state.generation == "" || state.generation == live.QuotaGeneration) {
		state.generation = live.QuotaGeneration
		return writeUserCache(fresh.ToBaseUser(), false)
	}
	// A batch hash must come from this guarded, fresh snapshot, not an old
	// hash lacking a generation or an asynchronous credit/debit callback.
	if err := invalidateUserCache(user.Id); err != nil {
		return err
	}
	if err := writeUserCache(fresh.ToBaseUser(), true); err != nil {
		return err
	}
	generation := common.GetUUID()
	if err := common.RDB.HSet(context.Background(), getUserCacheKey(user.Id), "QuotaGeneration", generation).Err(); err != nil {
		state.dirty = true
		return err
	}
	state.generation = generation
	state.dirty = false
	return nil
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	return writeUserCache(user.ToBaseUser(), false)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (*UserBase, error) {
	// Try getting from Redis first
	userCache, err := cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}
	userQuotaCacheMu.Lock()
	pendingQuota := batchUserQuotaPendingLocked(userId)
	userQuotaCacheMu.Unlock()
	if pendingQuota || errors.Is(err, ErrUserQuotaCacheUnavailable) || (common.BatchUpdateEnabled && !common.RedisEnabled) {
		return nil, ErrUserQuotaCacheUnavailable
	}

	// Batch quota must not fall back to a database snapshot while deltas are
	// pending. Authentication version fences retain their existing checks below.
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err
	}
	if common.RedisEnabled {
		floor, floorErr := getUserAuthVersionFloor(userId)
		if floorErr != nil && common.BatchUpdateEnabled {
			return nil, ErrUserQuotaCacheUnavailable
		}
		if floorErr == nil && floor > user.AuthVersion {
			return nil, ErrUserAuthCachePending
		}
		if err := populateUserCache(*user); err != nil {
			if errors.Is(err, ErrUserAuthCachePending) || errors.Is(err, ErrUserQuotaCacheUnavailable) {
				return nil, err
			}
			common.SysLog(common.LogText("failed to synchronously populate user cache: %s", err.Error()))
			if common.BatchUpdateEnabled {
				return nil, ErrUserQuotaCacheUnavailable
			}
		} else if common.BatchUpdateEnabled {
			return cacheGetUserBase(userId)
		}
	}
	return user.ToBaseUser(), nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id != userId || userCache.CacheSchema != userCacheSchemaVersion || userCache.AuthVersion <= 0 {
		return nil, fmt.Errorf("user cache schema is stale")
	}
	userQuotaCacheMu.Lock()
	state := batchUserQuotaStates[userId]
	blocked := state != nil && (state.dirty || state.hydrating || (state.generation != "" && state.generation != userCache.QuotaGeneration))
	recoverable := blocked && !state.hydrating && !batchUserQuotaPendingLocked(userId)
	userQuotaCacheMu.Unlock()
	if blocked {
		if recoverable {
			return nil, fmt.Errorf("user quota cache requires a fresh database snapshot")
		}
		return nil, ErrUserQuotaCacheUnavailable
	}
	if common.BatchUpdateEnabled && userCache.QuotaGeneration == "" {
		return nil, fmt.Errorf("user quota cache generation is missing")
	}
	floor, err := getUserAuthVersionFloor(userId)
	if err != nil {
		return nil, err
	}
	if floor > userCache.AuthVersion {
		return nil, ErrUserAuthCachePending
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields.
// 通过守卫式 Lua 脚本执行：哈希不存在时直接跳过（下次读取会从数据库水合），
// 不会像裸 HINCRBY 那样创建只含 Quota 字段的残缺哈希。
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	if common.BatchUpdateEnabled {
		userQuotaCacheMu.Lock()
		defer userQuotaCacheMu.Unlock()
		if state := batchUserQuotaStates[userId]; state != nil {
			state.revision++
			state.dirty = true
		}
		// Already-committed deltas can arrive after fresh cache hydration.
		// Queued deltas use queueUserQuotaDelta instead, without this callback.
		return invalidateUserCache(userId)
	}
	_, err := cacheApplyUserQuotaDelta(userId, delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// Committed credits update synchronous-mode caches. Batch mode invalidates the
// snapshot; queued deltas must finish before the committed balance is hydrated.
func syncCreditUserQuotaCache(userId int, quota int, operation string) {
	if quota <= 0 {
		return
	}
	if err := cacheIncrUserQuota(userId, int64(quota)); err != nil {
		common.SysLog(common.LogText("failed to sync %s credit to user quota cache: %s", operation, err.Error()))
	}
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// RefreshUserGroupCache writes the database-authoritative group into an
// existing user hash without changing the user's authentication version.
func RefreshUserGroupCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 {
		return fmt.Errorf("invalid user id")
	}
	var authoritative User
	if err := DB.Select("id", "auth_version", commonGroupCol).Where("id = ?", userId).First(&authoritative).Error; err != nil {
		return err
	}
	// Group transitions intentionally keep the same authentication version. A
	// refresh that read the previous group can therefore arrive after a newer
	// refresh and still pass the auth-version fence. Re-read after every write
	// and repair the cache when the authoritative group changed in between.
	for range 3 {
		if err := updateUserCacheFieldAtVersion(userId, "Group", authoritative.Group, authoritative.AuthVersion); err != nil {
			return err
		}

		var verified User
		if err := DB.Select("id", "auth_version", commonGroupCol).Where("id = ?", userId).First(&verified).Error; err != nil {
			return err
		}
		if verified.AuthVersion == authoritative.AuthVersion && verified.Group == authoritative.Group {
			return nil
		}
		authoritative = verified
	}

	// Preserve the freshest snapshot observed even when the row was too busy to
	// stabilize within the bounded retries. Returning an error lets best-effort
	// callers emit an operation-specific warning.
	if err := updateUserCacheFieldAtVersion(userId, "Group", authoritative.Group, authoritative.AuthVersion); err != nil {
		return err
	}
	return fmt.Errorf("user group changed repeatedly during cache refresh")
}

func updateUserEmailCache(userId int, email string) error {
	return updateUserCacheField(userId, "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	return updateUserCacheField(userId, "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	return updateUserCacheField(userId, "Setting", setting)
}

// updateUserCacheField prevents individual cache refreshes from bypassing the
// auth-version fence. It intentionally does nothing when the complete hash is
// absent; the next GetUserCache call will repopulate it from the database.
func updateUserCacheField(userId int, field string, value any) error {
	if !common.RedisEnabled {
		return nil
	}
	var user User
	if err := DB.Select("id", "auth_version").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.AuthVersion <= 0 {
		return fmt.Errorf("invalid user auth version")
	}
	return updateUserCacheFieldAtVersion(userId, field, value, user.AuthVersion)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
