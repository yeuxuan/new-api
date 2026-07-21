package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupEmailBindTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.BonusQuotaGrant{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestEmailBindAwardsConfiguredBonusQuotaOnce(t *testing.T) {
	db := setupEmailBindTestDB(t)
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

	user := model.User{
		Username: "email-bind-reward-user",
		Password: "password",
		Role:     common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	bindEmail := func(email, code string) *httptest.ResponseRecorder {
		common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
		t.Cleanup(func() { common.DeleteKey(email, common.EmailVerificationPurpose) })
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		body := fmt.Sprintf(`{"email":%q,"code":%q}`, email, code)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth/email/bind", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", user.Id)
		EmailBind(c)
		return recorder
	}

	before := time.Now().Unix()
	recorder := bindEmail("reward@example.com", "123456")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"quota_awarded":2500`)
	secondRecorder := bindEmail("reward-updated@example.com", "654321")
	assert.Equal(t, http.StatusOK, secondRecorder.Code)
	assert.Contains(t, secondRecorder.Body.String(), `"quota_awarded":0`)

	var grants []model.BonusQuotaGrant
	require.NoError(t, db.Where("user_id = ? AND source = ?", user.Id, model.BonusQuotaSourceEmailBind).Find(&grants).Error)
	require.Len(t, grants, 1)
	assert.Equal(t, 2500, grants[0].AmountTotal)
	assert.Equal(t, 2500, grants[0].AmountRemaining)
	assert.GreaterOrEqual(t, grants[0].ExpiresAt, before+7*86400)
}
