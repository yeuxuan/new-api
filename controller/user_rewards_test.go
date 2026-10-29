package controller

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserRewardsTest(t *testing.T) (*model.User, service.AuthIdentity) {
	t.Helper()
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.BonusQuotaGrant{}, &model.Checkin{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	oldEmail, oldBonus, oldCheckin := *operation_setting.GetEmailBindSetting(), *operation_setting.GetBonusQuotaSetting(), *operation_setting.GetCheckinSetting()
	*operation_setting.GetEmailBindSetting() = operation_setting.EmailBindSetting{Enabled: true, Quota: 500}
	*operation_setting.GetBonusQuotaSetting() = operation_setting.BonusQuotaSetting{ValidityDays: 7, AllowedModels: "reward-model"}
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, MinQuota: 100, MaxQuota: 100}
	t.Cleanup(func() {
		*operation_setting.GetEmailBindSetting(), *operation_setting.GetBonusQuotaSetting(), *operation_setting.GetCheckinSetting() = oldEmail, oldBonus, oldCheckin
	})
	return user, identity
}

func TestUserRewardsEmailOnceAcrossRebindAndDeletion(t *testing.T) {
	for _, status := range []string{model.BonusQuotaStatusActive, model.BonusQuotaStatusExpired, model.BonusQuotaStatusDepleted, model.BonusQuotaStatusCleared} {
		t.Run(status, func(t *testing.T) {
			user, identity := setupUserRewardsTest(t)
			mailbox := newSecurityMailbox(t)
			flow := startSecurityEmailBinding(t, identity, "first@example.com", service.VerificationMethodPassword)
			code := mailbox.code(t, flow.Email)
			request, err := common.Marshal(map[string]string{"flow_token": flow.FlowToken, "new_code": code})
			require.NoError(t, err)
			response := securityEnrollmentRequest(http.MethodPost, "/api/oauth/email/bind", string(request), "", identity, EmailBind)
			var result struct {
				Success   bool  `json:"success"`
				Quota     int   `json:"quota_awarded"`
				ExpiresAt int64 `json:"expires_at"`
				Data      struct {
					Quota     int   `json:"quota_awarded"`
					ExpiresAt int64 `json:"expires_at"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			assert.Equal(t, 500, result.Quota)
			assert.Equal(t, result.Quota, result.Data.Quota)
			assert.Equal(t, result.ExpiresAt, result.Data.ExpiresAt)
			assert.InDelta(t, time.Now().Add(7*24*time.Hour).Unix(), result.ExpiresAt, 5)
			_, err = service.FinishEmailBinding(identity, flow.FlowToken, code, "")
			assert.ErrorIs(t, err, model.ErrAuthFlowConsumed)
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.Zero(t, user.Quota, "rewards never enter the unrestricted wallet")
			allowed, err := model.GetAvailableBonusQuota(user.Id, "reward-model")
			require.NoError(t, err)
			assert.Equal(t, 500, allowed)
			other, err := model.GetAvailableBonusQuota(user.Id, "other-model")
			require.NoError(t, err)
			assert.Zero(t, other)

			flow = startSecurityEmailBinding(t, identity, "replacement@example.com", service.VerificationMethodPassword)
			state, err := service.FinishEmailBinding(identity, flow.FlowToken, mailbox.code(t, flow.Email), mailbox.code(t, "first@example.com"))
			require.NoError(t, err)
			assert.Zero(t, state.QuotaAwarded, "changing an address is not a first bind")
			require.NoError(t, model.DB.Model(&model.BonusQuotaGrant{}).Where("user_id = ?", user.Id).Update("status", status).Error)
			require.NoError(t, user.ClearBinding("email"))
			flow = startSecurityEmailBinding(t, identity, "rebound@example.com", service.VerificationMethodPassword)
			state, err = service.FinishEmailBinding(identity, flow.FlowToken, mailbox.code(t, flow.Email), "")
			require.NoError(t, err)
			assert.Zero(t, state.QuotaAwarded, "unlinking must not reset lifetime eligibility")
			proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeAccountDelete}, service.VerificationMethodPassword)
			response = securityEnrollmentRequest(http.MethodDelete, "/api/user/self", "", proof, identity, DeleteSelf)
			require.Contains(t, response.Body.String(), `"success":true`)
			var grants []model.BonusQuotaGrant
			require.NoError(t, model.DB.Where("user_id = ?", user.Id).Find(&grants).Error)
			require.Len(t, grants, 1, "deletion must not erase reward history")
			assert.Equal(t, 500, grants[0].AmountTotal)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			encoded, err := common.Marshal(audits)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), proof)
			assert.NotContains(t, string(encoded), code)
		})
	}
}

func TestUserRewardsEmailGrantFailureRollsBackAndCanRetry(t *testing.T) {
	user, identity := setupUserRewardsTest(t)
	mailbox := newSecurityMailbox(t)
	flow := startSecurityEmailBinding(t, identity, "first@example.com", service.VerificationMethodPassword)
	code := mailbox.code(t, flow.Email)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:fail_email_reward", func(tx *gorm.DB) {
		if tx.Statement.Table == "bonus_quota_grants" {
			tx.AddError(errors.New("reward storage unavailable"))
		}
	}))
	_, err := service.FinishEmailBinding(identity, flow.FlowToken, code, "")
	require.Error(t, err)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Empty(t, user.Email)
	pending, _, err := model.GetEmailBinding(identity, flow.FlowToken)
	require.NoError(t, err)
	assert.Nil(t, pending.ConsumedAt)
	require.NoError(t, model.DB.Callback().Create().Remove("test:fail_email_reward"))
	state, err := service.FinishEmailBinding(identity, flow.FlowToken, code, "")
	require.NoError(t, err)
	assert.Equal(t, 500, state.QuotaAwarded)
	_, err = service.FinishEmailBinding(identity, flow.FlowToken, code, "")
	assert.ErrorIs(t, err, model.ErrAuthFlowConsumed)
	var count int64
	require.NoError(t, model.DB.Model(&model.BonusQuotaGrant{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestUserRewardsEmailConcurrentConfirmationHasOneWinner(t *testing.T) {
	user, identity := setupUserRewardsTest(t)
	mailbox := newSecurityMailbox(t)
	flow := startSecurityEmailBinding(t, identity, "first@example.com", service.VerificationMethodPassword)
	code := mailbox.code(t, flow.Email)
	type result struct {
		state *model.EmailBindingState
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			state, err := service.FinishEmailBinding(identity, flow.FlowToken, code, "")
			results <- result{state, err}
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			winners++
			require.NotNil(t, result.state)
			assert.Equal(t, 500, result.state.QuotaAwarded)
		}
	}
	assert.Equal(t, 1, winners)
	var grants []model.BonusQuotaGrant
	require.NoError(t, model.DB.Where("user_id = ?", user.Id).Find(&grants).Error)
	require.Len(t, grants, 1)
	assert.Equal(t, 500, grants[0].AmountRemaining)
}

func TestUserRewardsDisabledEmailRewardDoesNotBackfillOnChange(t *testing.T) {
	user, identity := setupUserRewardsTest(t)
	mailbox := newSecurityMailbox(t)
	operation_setting.GetEmailBindSetting().Enabled = false
	flow := startSecurityEmailBinding(t, identity, "first@example.com", service.VerificationMethodPassword)
	state, err := service.FinishEmailBinding(identity, flow.FlowToken, mailbox.code(t, flow.Email), "")
	require.NoError(t, err)
	assert.Zero(t, state.QuotaAwarded)
	operation_setting.GetEmailBindSetting().Enabled = true
	flow = startSecurityEmailBinding(t, identity, "changed@example.com", service.VerificationMethodPassword)
	state, err = service.FinishEmailBinding(identity, flow.FlowToken, mailbox.code(t, flow.Email), mailbox.code(t, "first@example.com"))
	require.NoError(t, err)
	assert.Zero(t, state.QuotaAwarded)
	var count int64
	require.NoError(t, model.DB.Model(&model.BonusQuotaGrant{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUserRewardsEmailRejectsInvalidAuthorization(t *testing.T) {
	for _, scenario := range []string{"wrong code", "expired", "other session", "deleted account", "attempt limit", "missing proof", "legacy body"} {
		t.Run(scenario, func(t *testing.T) {
			user, identity := setupUserRewardsTest(t)
			mailbox := newSecurityMailbox(t)
			if scenario == "missing proof" || scenario == "legacy body" {
				handler, path := EmailBindStart, "/api/oauth/email/bind/start"
				if scenario == "legacy body" {
					handler, path = EmailBind, "/api/oauth/email/bind"
				}
				response := securityEnrollmentRequest(http.MethodPost, path, `{"email":"new@example.com","code":"123456"}`, "", identity, handler)
				assert.Contains(t, response.Body.String(), `"success":false`)
			} else {
				flow := startSecurityEmailBinding(t, identity, "new@example.com", service.VerificationMethodPassword)
				code := mailbox.code(t, flow.Email)
				switch scenario {
				case "wrong code":
					code = "invalid"
				case "expired":
					stored, _, err := model.GetEmailBinding(identity, flow.FlowToken)
					require.NoError(t, err)
					require.NoError(t, model.DB.Model(stored).Update("expires_at", time.Now().Add(-time.Minute)).Error)
				case "other session":
					identity.SessionID = "another-session"
				case "deleted account":
					_, err := user.Delete()
					require.NoError(t, err)
				case "attempt limit":
					for range model.EmailBindingMaxAttempts {
						_, err := service.FinishEmailBinding(identity, flow.FlowToken, "invalid", "")
						require.Error(t, err)
					}
				}
				_, err := service.FinishEmailBinding(identity, flow.FlowToken, code, "")
				require.Error(t, err)
			}
			var count int64
			require.NoError(t, model.DB.Model(&model.BonusQuotaGrant{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, model.DB.Unscoped().First(user, user.Id).Error)
			assert.Empty(t, user.Email)
			assert.Zero(t, user.Quota)
		})
	}
}

func TestUserRewardsCheckinAtomicAndFilteredClear(t *testing.T) {
	user, _ := setupUserRewardsTest(t)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:fail_checkin_reward", func(tx *gorm.DB) {
		if tx.Statement.Table == "bonus_quota_grants" {
			tx.AddError(errors.New("reward storage unavailable"))
		}
	}))
	_, err := model.UserCheckin(user.Id)
	require.Error(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.Checkin{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, model.DB.Callback().Create().Remove("test:fail_checkin_reward"))
	checkin, err := model.UserCheckin(user.Id)
	require.NoError(t, err)
	_, err = model.UserCheckin(user.Id)
	require.Error(t, err)
	_, _, err = model.ConsumeBonusQuota(user.Id, "reward-model", 30)
	require.NoError(t, err)
	_, err = model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceEmailBind, "", 500, 0)
	require.NoError(t, err)
	_, err = model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceCheckin, "2000-01-01", 200, 0)
	require.NoError(t, err)
	other := model.User{Username: "other-reward-user", AffCode: "other-reward", Quota: 1000}
	require.NoError(t, model.DB.Create(&other).Error)
	_, err = model.CreateBonusQuotaGrant(other.Id, model.BonusQuotaSourceCheckin, checkin.CheckinDate, 300, 0)
	require.NoError(t, err)
	preview, err := model.PreviewClearCheckinQuota(checkin.CheckinDate, checkin.CheckinDate, []int{user.Id})
	require.NoError(t, err)
	require.Len(t, preview, 1)
	assert.Equal(t, 70, preview[0].ActualClear)
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("test:fail_checkin_clear", func(tx *gorm.DB) {
		if tx.Statement.Table == "bonus_quota_grants" {
			tx.AddError(errors.New("clear storage unavailable"))
		}
	}))
	_, _, err = model.BatchClearCheckinQuota(checkin.CheckinDate, checkin.CheckinDate, []int{user.Id}, "operator")
	require.Error(t, err)
	require.NoError(t, model.DB.First(checkin, checkin.Id).Error)
	assert.False(t, checkin.Cleared)
	require.NoError(t, model.DB.Callback().Update().Remove("test:fail_checkin_clear"))
	users, total, err := model.BatchClearCheckinQuota(checkin.CheckinDate, checkin.CheckinDate, []int{user.Id}, "operator")
	require.NoError(t, err)
	assert.Equal(t, 1, users)
	assert.Equal(t, 70, total)
	require.NoError(t, model.DB.First(checkin, checkin.Id).Error)
	assert.True(t, checkin.Cleared)
	users, total, err = model.BatchClearCheckinQuota(checkin.CheckinDate, checkin.CheckinDate, []int{user.Id}, "operator")
	require.NoError(t, err)
	assert.Zero(t, users)
	assert.Zero(t, total)
	remaining, err := model.GetUserBonusQuotaTotal(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 700, remaining)
	remaining, err = model.GetUserBonusQuotaTotal(other.Id)
	require.NoError(t, err)
	assert.Equal(t, 300, remaining)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota)
	logs, count, err := model.GetUserLogs(user.Id, model.LogFilter{Type: model.LogTypeSystem}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	require.Len(t, logs, 1)
	assert.Equal(t, -70, logs[0].Quota)
	assert.NotContains(t, logs[0].Content, "operator")
	stats, err := model.GetUserCheckinStats(user.Id, time.Now().Format("2006-01"))
	require.NoError(t, err)
	assert.Equal(t, 700, stats["bonus_quota"])
	assert.Equal(t, []string{"reward-model"}, stats["bonus_quota_allowed_models"])
	assert.Equal(t, 7, stats["bonus_quota_validity_days"])
	assert.EqualValues(t, 100, stats["total_quota"])
	assert.True(t, stats["checked_in_today"].(bool))
}

func TestUserRewardsCheckinExpiredBalanceIsNotClearedTwice(t *testing.T) {
	user, _ := setupUserRewardsTest(t)
	date := time.Now().Format("2006-01-02")
	_, err := model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceCheckin, date, 100, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, err)
	preview, err := model.PreviewClearCheckinQuota(date, date, nil)
	require.NoError(t, err)
	assert.Empty(t, preview)
	users, total, err := model.BatchClearCheckinQuota(date, date, nil, "operator")
	require.NoError(t, err)
	assert.Zero(t, users)
	assert.Zero(t, total)
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
	assert.Zero(t, count)
	for _, body := range []string{
		`{"start_date":"invalid","end_date":"2000-01-01"}`,
		`{"start_date":"2000-01-02","end_date":"2000-01-01"}`,
		fmt.Sprintf(`{"start_date":%q,"end_date":%q}`, time.Now().AddDate(0, 0, 1).Format("2006-01-02"), time.Now().AddDate(0, 0, 1).Format("2006-01-02")),
	} {
		response := securityEnrollmentRequest(http.MethodPost, "/api/user/checkin/clear", body, "", service.AuthIdentity{UserID: user.Id}, AdminClearCheckinQuota)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
}

func TestUserRewardsCheckinClearRetiresFullyReservedGrant(t *testing.T) {
	user, _ := setupUserRewardsTest(t)
	checkin, err := model.UserCheckin(user.Id)
	require.NoError(t, err)
	_, consumed, err := model.ConsumeBonusQuota(user.Id, "reward-model", checkin.QuotaAwarded)
	require.NoError(t, err)
	assert.Equal(t, checkin.QuotaAwarded, consumed)
	users, total, err := model.BatchClearCheckinQuota(checkin.CheckinDate, checkin.CheckinDate, []int{user.Id}, "operator")
	require.NoError(t, err)
	assert.Zero(t, users)
	assert.Zero(t, total)
	var grant model.BonusQuotaGrant
	require.NoError(t, model.DB.Where("user_id = ? AND source = ?", user.Id, model.BonusQuotaSourceCheckin).First(&grant).Error)
	assert.Equal(t, model.BonusQuotaStatusCleared, grant.Status)
}
