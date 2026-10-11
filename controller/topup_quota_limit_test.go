package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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
	assert.JSONEq(t, `{"message":"success","data":"0.01","group_ratio":1}`, response.Body.String())
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota, "amount previews must not credit the wallet")
}

func setupIPayNowTopUpTest(t *testing.T) (*model.User, service.AuthIdentity) {
	t.Helper()
	user, identity := setupUserRewardsTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	confirmPaymentComplianceForTest(t)
	originalID, originalKey := operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey
	originalMin, originalUnit, originalPrice := operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price
	originalDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalRatios, originalDiscounts := common.TopupGroupRatio2JSONString(), operation_setting.GetPaymentSetting().AmountDiscount
	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey = originalID, originalKey
		operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price = originalMin, originalUnit, originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplay
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		http.DefaultTransport = originalTransport
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalRatios))
	})
	operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey = "app-id", "test-key"
	operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price = 1, 500000, 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountDiscount = nil
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	return user, identity
}

func TestTopUpHistoryProjectsOnlyValidIPayNowQuotaSnapshots(t *testing.T) {
	user, identity := setupIPayNowTopUpTest(t)
	common.QuotaPerUnit = 1000000 // History must read the snapshot, not infer quota from Amount.
	for _, tc := range []struct {
		name, method, provider, payload string
		wantQuota                       int
	}{
		{"exact", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"version":1,"credited_quota":750000}`, 750000},
		{"maximum", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, fmt.Sprintf(`{"version":1,"credited_quota":%d}`, common.MaxWalletQuota), common.MaxWalletQuota},
		{"legacy", model.PaymentMethodIPayNow, "", "", 0},
		{"other-provider", model.PaymentMethodCreem, model.PaymentProviderCreem, `{"version":1,"credited_quota":750000}`, 0},
		{"mismatched-provider", model.PaymentMethodIPayNow, model.PaymentProviderStripe, `{"version":1,"credited_quota":750000}`, 0},
		{"corrupt", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"secret":"never-expose-this",`, 0},
		{"unsupported", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"version":2,"credited_quota":750000}`, 0},
		{"negative", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"version":1,"credited_quota":-1}`, 0},
		{"missing", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"version":1}`, 0},
		{"fractional", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, `{"version":1,"credited_quota":1.5}`, 0},
		{"overflow", model.PaymentMethodIPayNow, model.PaymentProviderIPayNow, fmt.Sprintf(`{"version":1,"credited_quota":%d}`, common.MaxWalletQuota+1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := model.TopUp{UserId: user.Id, Amount: 1, Money: 0.96, TradeNo: "history-" + tc.name, PaymentMethod: tc.method, PaymentProvider: tc.provider, ProviderPayload: tc.payload, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusSuccess}
			require.NoError(t, order.Insert())
			for _, endpoint := range []struct {
				path    string
				handler gin.HandlerFunc
			}{
				{"/api/user/topup/self?p=1&page_size=100", GetUserTopUps},
				{"/api/user/topup?p=1&page_size=100", GetAllTopUps},
			} {
				for _, query := range []string{"", "&keyword=" + order.TradeNo} {
					response := securityEnrollmentRequest(http.MethodGet, endpoint.path+query, "", "", identity, endpoint.handler)
					var body struct {
						Success bool `json:"success"`
						Data    struct {
							Items []struct {
								Id            int     `json:"id"`
								Amount        int64   `json:"amount"`
								Money         float64 `json:"money"`
								CreditedQuota *int    `json:"credited_quota"`
							} `json:"items"`
						} `json:"data"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
					require.True(t, body.Success, response.Body.String())
					found := false
					for _, item := range body.Data.Items {
						if item.Id != order.Id {
							continue
						}
						found = true
						assert.Equal(t, int64(1), item.Amount)
						assert.Equal(t, 0.96, item.Money)
						if tc.wantQuota == 0 {
							assert.Nil(t, item.CreditedQuota)
						} else {
							require.NotNil(t, item.CreditedQuota)
							assert.Equal(t, tc.wantQuota, *item.CreditedQuota)
						}
					}
					require.True(t, found, endpoint.path+query)
					assert.NotContains(t, response.Body.String(), "provider_payload")
					assert.NotContains(t, response.Body.String(), "never-expose-this")
					if query != "" && tc.wantQuota == 0 {
						assert.NotContains(t, response.Body.String(), "credited_quota")
					}
				}
			}
			stored := model.GetTopUpById(order.Id)
			require.NotNil(t, stored)
			assert.Equal(t, order.Amount, stored.Amount)
			assert.Equal(t, tc.payload, stored.ProviderPayload)
		})
	}
}

func TestIPayNowPaymentFenBoundaries(t *testing.T) {
	user, identity := setupIPayNowTopUpTest(t)
	for _, tc := range []struct {
		name  string
		price float64
		fen   int64
	}{
		{"below half a cent", 0.0049, 0},
		{"half a cent rounds up", 0.005, 1},
		{"below one cent rounds up", 0.0099, 1},
		{"exactly one cent", 0.01, 1},
		{"below one and a half cents", 0.0149, 1},
		{"one and a half cents rounds up", 0.015, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.Price = tc.price
			calls := 0
			http.DefaultTransport = creemCheckoutTestTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				require.NoError(t, request.ParseForm())
				assert.Equal(t, fmt.Sprint(tc.fen), request.Form.Get("mhtOrderAmt"))
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("responseCode=A001&tn=https%3A%2F%2Fexample.test%2Fqr"))}, nil
			})
			preview := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/amount", `{"amount":1}`, "", identity, RequestIPayNowAmount)
			payment := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/pay", `{"amount":1,"payment_method":"ipaynow"}`, "", identity, RequestIPayNowPay)
			if tc.fen == 0 {
				assert.JSONEq(t, `{"message":"error","data":"Payment amount is too low","message_key":"Payment amount is too low"}`, preview.Body.String())
				assert.JSONEq(t, preview.Body.String(), payment.Body.String())
				assert.Zero(t, calls)
				return
			}
			assert.JSONEq(t, fmt.Sprintf(`{"message":"success","data":"%s","group_ratio":1}`, decimal.NewFromInt(tc.fen).Div(decimal.NewFromInt(100)).StringFixed(2)), preview.Body.String())
			var response struct {
				Message string `json:"message"`
				Data    struct {
					TradeNo string `json:"trade_no"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(payment.Body.Bytes(), &response))
			require.Equal(t, "success", response.Message, payment.Body.String())
			assert.Equal(t, 1, calls)
			order := model.GetTopUpByTradeNo(response.Data.TradeNo)
			require.NotNil(t, order)
			assert.Equal(t, float64(tc.fen)/100, order.Money)
		})
	}
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota, "creating orders must not credit the wallet")
}

func TestIPayNowTokenQuotaSnapshotSettlement(t *testing.T) {
	for _, settlement := range []string{"signed notification", "active query", "manual completion"} {
		t.Run(settlement, func(t *testing.T) {
			user, identity := setupIPayNowTopUpTest(t)
			operation_setting.IPayNowMinTopUp, common.QuotaPerUnit, operation_setting.Price = 1, 500000, 0.8
			operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
			operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{750000: 0.5}
			require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1.25}`))
			http.DefaultTransport = creemCheckoutTestTransport(func(request *http.Request) (*http.Response, error) {
				require.NoError(t, request.ParseForm())
				body := "responseCode=A001&tn=https%3A%2F%2Fexample.test%2Fqr"
				if request.Form.Get("funcode") == ipaynow.FuncQueryOrder {
					body = "responseCode=A001&transStatus=A001&mhtOrderAmt=75"
				} else {
					assert.Equal(t, "75", request.Form.Get("mhtOrderAmt"))
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			response := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/pay", `{"amount":750000,"payment_method":"ipaynow"}`, "", identity, RequestIPayNowPay)
			require.Contains(t, response.Body.String(), `"message":"success"`)
			var order model.TopUp
			require.NoError(t, model.DB.Where("user_id = ?", user.Id).First(&order).Error)
			assert.EqualValues(t, 1, order.Amount, "legacy display amount keeps its existing meaning")
			assert.Equal(t, 0.75, order.Money, "discounted price is not the credited quantity")
			assert.JSONEq(t, `{"version":1,"credited_quota":750000}`, order.ProviderPayload)
			publicOrder, err := common.Marshal(order)
			require.NoError(t, err)
			assert.NotContains(t, string(publicOrder), "provider_payload")
			assert.NotContains(t, string(publicOrder), "credited_quota")
			_, err = model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceEmailBind, "", 500, 0)
			require.NoError(t, err)
			common.QuotaPerUnit = 1000000
			operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
			switch settlement {
			case "signed notification":
				params := map[string]string{"appId": "app-id", "mhtOrderNo": order.TradeNo, "mhtOrderAmt": "75", "transStatus": ipaynow.TransStatusSuccess}
				params["signature"] = ipaynow.Sign(params, "test-key")
				form := url.Values{}
				for key, value := range params {
					form.Set(key, value)
				}
				for range 2 {
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodPost, "/api/ipaynow/notify", strings.NewReader(form.Encode()))
					IPayNowNotify(ctx)
					assert.Equal(t, "success=Y", recorder.Body.String())
				}
			case "active query":
				require.NoError(t, model.DB.Model(&order).Update("create_time", time.Now().Unix()-ipaynowActiveQueryMinDelaySec).Error)
				require.True(t, tryActiveQueryIPayNow(order.TradeNo))
				ipaynowQueryCooldown.Delete(order.TradeNo)
			case "manual completion":
				for range 2 {
					require.NoError(t, model.ManualCompleteTopUp(order.TradeNo, "127.0.0.1"))
				}
			}
			alreadyDone, err := model.RechargeIPayNow(order.TradeNo, "127.0.0.1")
			require.NoError(t, err)
			assert.True(t, alreadyDone)
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.Equal(t, 750000, user.Quota)
			bonus, err := model.GetUserBonusQuotaTotal(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 500, bonus)
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, 750000, logs[0].Quota)
		})
	}
}

func TestIPayNowRejectsInvalidSnapshotAndExactQuotaOverflow(t *testing.T) {
	user, identity := setupIPayNowTopUpTest(t)
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	require.NoError(t, model.DB.Model(user).Update("quota", common.MaxWalletQuota-600000).Error)
	for _, handler := range []gin.HandlerFunc{RequestIPayNowAmount, RequestIPayNowPay} {
		response := securityEnrollmentRequest(http.MethodPost, "/api/user/ipaynow/pay", `{"amount":750000,"payment_method":"ipaynow"}`, "", identity, handler)
		assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, response.Body.String())
	}
	for index, payload := range []string{`{`, `{"version":2,"credited_quota":750000}`, `{"version":1,"credited_quota":0}`, `{"version":1,"credited_quota":9007199254740992}`} {
		order := model.TopUp{UserId: user.Id, Amount: 1, Money: 1, TradeNo: fmt.Sprintf("IPN-invalid-%d", index), PaymentMethod: model.PaymentMethodIPayNow, PaymentProvider: model.PaymentProviderIPayNow, ProviderPayload: payload, Status: common.TopUpStatusPending}
		require.NoError(t, model.DB.Create(&order).Error)
		_, err := model.RechargeIPayNow(order.TradeNo, "127.0.0.1")
		require.ErrorIs(t, err, model.ErrInvalidTopUpQuota)
		require.ErrorIs(t, model.ManualCompleteTopUp(order.TradeNo, "127.0.0.1"), model.ErrInvalidTopUpQuota)
		require.NoError(t, model.DB.First(&order, order.Id).Error)
		assert.Equal(t, common.TopUpStatusPending, order.Status)
	}
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, common.MaxWalletQuota-600000, user.Quota)
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
	// Existing rows receive NULL when the nullable payload column is added.
	require.NoError(t, model.DB.Model(&order).Update("provider_payload", nil).Error)
	legacyOrder := model.GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, legacyOrder)
	assert.Empty(t, legacyOrder.ProviderPayload)
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
