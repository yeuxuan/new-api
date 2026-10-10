package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/ipaynow"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTopUpQuotaValidation(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	testCases := []struct {
		name        string
		displayType string
		amount      int64
		wantQuota   int
		wantErr     bool
	}{
		{
			name:        "currency amount below limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4294,
			wantQuota:   2_147_000_000,
		},
		{
			name:        "currency amount above limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4295,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount preserves settlement truncation",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      2_147_500_000,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount above legacy int32 range",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      4_294_500_000,
			wantQuota:   4_294_500_000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			quota, err := getTopUpQuota(tc.amount)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}

func TestValidateTopUpQuotaReturnsMaximumAmount(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()

	_, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	_, err = validateTopUpQuota(maxAmount + 1)
	require.EqualError(t, err, fmt.Sprintf("Top-up amount cannot exceed %d", maxAmount))
}

func TestRequestAmountRejectsTopUpThatCannotBeSettled(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(fmt.Sprintf(`{"amount":%d}`, maxAmount+1)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"message":"error","data":"Top-up amount cannot exceed %d","message_key":"Top-up amount cannot exceed {{max}}","message_params":{"max":%d}}`, maxAmount, maxAmount), recorder.Body.String())
}

func TestRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       42,
		Username: "topup_capacity_user",
		Quota:    common.MaxWalletQuota - 100_000,
		Status:   common.UserStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(`{"amount":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, recorder.Body.String())
}

func TestIPayNowRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldMinTopup := operation_setting.IPayNowMinTopUp
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.IPayNowMinTopUp = 1

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		operation_setting.IPayNowMinTopUp = oldMinTopup
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       43,
		Username: "ipaynow_capacity_user",
		Quota:    common.MaxWalletQuota - 1_000_000,
		Status:   common.UserStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 43)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/ipaynow/amount",
		strings.NewReader(`{"amount":3}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestIPayNowAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, recorder.Body.String())
}

func TestValidateCreditedQuotaRejectsOverflow(t *testing.T) {
	_, err := validateCreditedQuota(decimal.NewFromInt(int64(common.MaxWalletQuota / 2)))
	require.NoError(t, err)
	_, err = validateCreditedQuota(decimal.Zero)
	require.EqualError(t, err, "Top-up quota must be greater than 0")
	_, err = validateCreditedQuota(decimal.NewFromInt(common.MaxWalletQuota + 1))
	require.EqualError(
		t,
		err,
		"Top-up quota exceeds the range the system can represent",
	)
}

func TestStripeCreditedQuotaIncludesGroupRatio(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldTopupGroupRatio := common.TopupGroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"vip":2}`))
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldTopupGroupRatio))
	})

	_, err := validateCreditedQuota(getStripeCreditedQuota(2147, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(2148, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(int64(common.MaxWalletQuota), "vip"))
	require.Error(t, err)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	assert.True(t, decimal.NewFromInt(500000).Equal(getStripeCreditedQuota(1, "free")))
}

func TestCreemCreditedQuotaUsesUSDEquivalent(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
	})

	quota, err := getCreemCreditedQuota(0.14)
	require.NoError(t, err)
	assert.Equal(t, 70000, quota)

	quota, err = getCreemCreditedQuota(4295)
	require.NoError(t, err)
	assert.Equal(t, 2_147_500_000, quota)

	_, err = getCreemCreditedQuota(float64(common.MaxWalletQuota)/common.QuotaPerUnit + 1)
	require.EqualError(t, err, "Top-up quota exceeds the range the system can represent")
}

func TestIPayNowPaymentErrorContracts(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalID, originalKey := operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey
	originalMin, originalUnit := operation_setting.IPayNowMinTopUp, common.QuotaPerUnit
	originalDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey = originalID, originalKey
		operation_setting.IPayNowMinTopUp, common.QuotaPerUnit = originalMin, originalUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplay
	})
	common.QuotaPerUnit = 100
	for _, tc := range []struct {
		name, body, key, display, messageKey, data string
		minimum, expectedMin                       int
		handler                                    gin.HandlerFunc
	}{
		{"invalid payment JSON", `{`, "key", operation_setting.QuotaDisplayTypeUSD, "Invalid parameters", "Invalid parameters", 2, 0, RequestIPayNowPay},
		{"invalid amount JSON", `{`, "key", operation_setting.QuotaDisplayTypeUSD, "Invalid parameters", "Invalid parameters", 2, 0, RequestIPayNowAmount},
		{"unsupported channel", `{"amount":2,"payment_method":"creem"}`, "key", operation_setting.QuotaDisplayTypeUSD, "Unsupported payment channel", "Unsupported payment channel", 2, 0, RequestIPayNowPay},
		{"missing payment key", `{"amount":2,"payment_method":"ipaynow"}`, "", operation_setting.QuotaDisplayTypeUSD, "iPayNow payment is not configured", "iPayNow payment is not configured", 2, 0, RequestIPayNowPay},
		{"currency minimum", `{"amount":1,"payment_method":"ipaynow"}`, "key", operation_setting.QuotaDisplayTypeUSD, "Top-up amount cannot be less than {{min}}", "Top-up amount cannot be less than 2", 2, 2, RequestIPayNowPay},
		{"token minimum", `{"amount":199}`, "key", operation_setting.QuotaDisplayTypeTokens, "Top-up amount cannot be less than {{min}}", "Top-up amount cannot be less than 200", 2, 200, RequestIPayNowAmount},
		{"unset minimum fallback", `{"amount":0}`, "key", operation_setting.QuotaDisplayTypeUSD, "Top-up amount cannot be less than {{min}}", "Top-up amount cannot be less than 1", 0, 1, RequestIPayNowAmount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey = "app-id", tc.key
			operation_setting.IPayNowMinTopUp = tc.minimum
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.display
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/ipaynow/pay", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			tc.handler(ctx)
			expected := gin.H{"message": "error", "data": tc.data, "message_key": tc.messageKey}
			if tc.expectedMin > 0 {
				expected["message_params"] = gin.H{"min": tc.expectedMin}
			}
			encoded, err := common.Marshal(expected)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, string(encoded), recorder.Body.String())
		})
	}
}

func TestIPayNowAmountPreservesDiscountAndGroupRatio(t *testing.T) {
	user, identity := setupUserRewardsTest(t)
	originalMin, originalUnit, originalPrice := operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price
	originalDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalRatios := common.TopupGroupRatio2JSONString()
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price = originalMin, originalUnit, originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplay
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalRatios))
	})
	operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price = 2, 100, 0.8
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1.25}`))
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{6: 0.5, 600: 0.5}
	for _, tc := range []struct {
		display string
		amount  int
	}{{operation_setting.QuotaDisplayTypeUSD, 6}, {operation_setting.QuotaDisplayTypeTokens, 600}} {
		t.Run(tc.display, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.display
			response := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/amount", fmt.Sprintf(`{"amount":%d}`, tc.amount), "", identity, RequestIPayNowAmount)
			assert.JSONEq(t, `{"message":"success","data":"3.00","group_ratio":1.25}`, response.Body.String())
		})
	}
	operation_setting.IPayNowMinTopUp, operation_setting.Price = 1, 0.01
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountDiscount = nil
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	response := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/amount", `{"amount":1}`, "", identity, RequestIPayNowAmount)
	assert.JSONEq(t, `{"message":"error","data":"Payment amount is too low","message_key":"Payment amount is too low"}`, response.Body.String())
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota, "amount previews must not credit the wallet")
}

func TestIPayNowSignedCallbackAndOrderQueryContracts(t *testing.T) {
	user, _ := setupUserRewardsTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	confirmPaymentComplianceForTest(t)
	originalID, originalKey, originalUnit := operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey, common.QuotaPerUnit
	t.Cleanup(func() {
		operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey, common.QuotaPerUnit = originalID, originalKey, originalUnit
	})
	operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey, common.QuotaPerUnit = "app-id", "test-key", 100
	_, err := model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceEmailBind, "", 500, 0)
	require.NoError(t, err)
	order := model.TopUp{UserId: user.Id, Amount: 2, Money: 1.23, TradeNo: "ipaynow-contract", PaymentMethod: model.PaymentMethodIPayNow, PaymentProvider: model.PaymentProviderIPayNow, Status: common.TopUpStatusPending}
	require.NoError(t, model.DB.Create(&order).Error)
	for _, tc := range []struct {
		name, signature, amount, status, reply string
		quota                                  int
	}{
		{"missing signature", "", "123", ipaynow.TransStatusSuccess, "success=N", 0},
		{"invalid signature", "invalid", "123", ipaynow.TransStatusSuccess, "success=N", 0},
		{"wrong signed amount", "signed", "124", ipaynow.TransStatusSuccess, "success=N", 0},
		{"signed pending", "signed", "123", ipaynow.TransStatusPending, "success=Y", 0},
		{"signed paid", "signed", "123", ipaynow.TransStatusSuccess, "success=Y", 200},
		{"duplicate paid", "signed", "123", ipaynow.TransStatusSuccess, "success=Y", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]string{"appId": "app-id", "mhtOrderNo": order.TradeNo, "mhtOrderAmt": tc.amount, "transStatus": tc.status}
			if tc.signature == "signed" {
				params["signature"] = ipaynow.Sign(params, "test-key")
			} else if tc.signature != "" {
				params["signature"] = tc.signature
			}
			form := url.Values{}
			for key, value := range params {
				form.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/ipaynow/notify", strings.NewReader(form.Encode()))
			IPayNowNotify(ctx)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tc.reply, recorder.Body.String())
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.Equal(t, tc.quota, user.Quota)
		})
	}
	bonus, err := model.GetUserBonusQuotaTotal(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 500, bonus, "paid recharge must not consume restricted rewards")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 200, logs[0].Quota)
	for _, tc := range []struct {
		name, tradeNo, expected string
		userID                  int
	}{
		{"missing number", "", `{"success":false,"message":"Order number not provided","message_key":"Order number not provided"}`, user.Id},
		{"missing order", "unknown", `{"success":false,"message":"Top-up order does not exist","message_key":"Top-up order does not exist"}`, user.Id},
		{"another user", order.TradeNo, `{"success":false,"message":"No permission to access this order","message_key":"No permission to access this order"}`, user.Id + 1},
		{"owner", order.TradeNo, `{"success":true,"message":"","data":{"status":"success","money":1.23,"trade_no":"ipaynow-contract"}}`, user.Id},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/ipaynow/order/"+tc.tradeNo, nil)
			ctx.Params = gin.Params{{Key: "trade_no", Value: tc.tradeNo}}
			ctx.Set("id", tc.userID)
			QueryIPayNowOrder(ctx)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, tc.expected, recorder.Body.String())
		})
	}
}
