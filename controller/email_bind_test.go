package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailBindAwardsConfiguredBonusQuotaOnce(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.BonusQuotaGrant{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	emailSetting := operation_setting.GetEmailBindSetting()
	bonusSetting := operation_setting.GetBonusQuotaSetting()
	previousEmailSetting := *emailSetting
	previousBonusSetting := *bonusSetting
	emailSetting.Enabled = true
	emailSetting.Quota = 2500
	bonusSetting.ValidityDays = 7
	t.Cleanup(func() {
		*emailSetting = previousEmailSetting
		*bonusSetting = previousBonusSetting
	})

	mailbox := newSecurityMailbox(t)
	startBinding := func(email string) service.EmailBindingData {
		flow := startSecurityEmailBinding(t, identity, email, service.VerificationMethodPassword)
		return flow
	}
	finishBinding := func(flow service.EmailBindingData, oldEmail string) *securityEnrollmentResponse {
		payload, err := common.Marshal(map[string]string{
			"flow_token": flow.FlowToken,
			"new_code":   mailbox.code(t, flow.Email),
			"old_code": func() string {
				if oldEmail == "" {
					return ""
				}
				return mailbox.code(t, oldEmail)
			}(),
		})
		require.NoError(t, err)
		response := securityEnrollmentRequest(http.MethodPost, "/api/oauth/email/bind", string(payload), "", identity, EmailBind)
		var result securityEnrollmentResponse
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.Equal(t, http.StatusOK, response.Code)
		return &result
	}

	before := time.Now().Unix()
	first := finishBinding(startBinding("reward@example.com"), "")
	assert.True(t, first.Success)
	var firstData struct {
		QuotaAwarded int64 `json:"quota_awarded"`
		ExpiresAt    int64 `json:"expires_at"`
	}
	require.NoError(t, common.Unmarshal(first.Data, &firstData))
	assert.Equal(t, int64(2500), firstData.QuotaAwarded)
	assert.GreaterOrEqual(t, firstData.ExpiresAt, before+7*86400)
	second := finishBinding(startBinding("reward-updated@example.com"), "reward@example.com")
	assert.True(t, second.Success)
	var secondData struct {
		QuotaAwarded int64 `json:"quota_awarded"`
	}
	require.NoError(t, common.Unmarshal(second.Data, &secondData))
	assert.Zero(t, secondData.QuotaAwarded)

	var grants []model.BonusQuotaGrant
	require.NoError(t, db.Where("user_id = ? AND source = ?", user.Id, model.BonusQuotaSourceEmailBind).Find(&grants).Error)
	require.Len(t, grants, 1)
	assert.Equal(t, 2500, grants[0].AmountTotal)
	assert.Equal(t, 2500, grants[0].AmountRemaining)
	assert.GreaterOrEqual(t, grants[0].ExpiresAt, before+7*86400)
}
