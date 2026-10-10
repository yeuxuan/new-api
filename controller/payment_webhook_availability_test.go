package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	setting.StripeWebhookSecret = ""
	setting.StripeApiSecret = "sk_test_123"
	setting.StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	setting.StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	setting.StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemAvailabilitySeparatesCatalogCheckoutAndCallback(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalHidden := common.HideOnlineTopUp
	common.HideOnlineTopUp = true
	t.Cleanup(func() { common.HideOnlineTopUp = originalHidden })
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
	})

	for _, tc := range []struct {
		name, key, products, secret, terms      string
		confirmed, topup, subscription, webhook bool
	}{
		{"configured", "key", `[{"productId":"prod"}]`, "secret", operation_setting.CurrentComplianceTermsVersion, true, true, true, true},
		{"no recharge catalog", "key", "[]", "secret", operation_setting.CurrentComplianceTermsVersion, true, false, true, true},
		{"settle existing orders without API key", "", "", "secret", operation_setting.CurrentComplianceTermsVersion, true, false, false, true},
		{"missing webhook secret with recharge catalog", "key", `[{"productId":"prod"}]`, "", operation_setting.CurrentComplianceTermsVersion, true, false, false, false},
		{"blank webhook secret with recharge catalog", "key", `[{"productId":"prod"}]`, " ", operation_setting.CurrentComplianceTermsVersion, true, false, false, false},
		{"unconfirmed compliance", "key", "[]", "secret", operation_setting.CurrentComplianceTermsVersion, false, false, false, false},
		{"outdated compliance", "key", "[]", "secret", "old-terms", true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = tc.key, tc.products, tc.secret
			payment := operation_setting.GetPaymentSetting()
			payment.ComplianceConfirmed, payment.ComplianceTermsVersion = tc.confirmed, tc.terms
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			GetTopUpInfo(ctx)
			var result struct {
				Data struct {
					Topup        bool `json:"enable_creem_topup"`
					Subscription bool `json:"enable_creem_subscription"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
			assert.Equal(t, tc.topup, result.Data.Topup)
			assert.Equal(t, tc.subscription, result.Data.Subscription)
			response := sendCreemWebhookForTest(t, CreemWebhookEvent{EventType: "ignored"}, true)
			expectedStatus := http.StatusForbidden
			if tc.webhook {
				expectedStatus = http.StatusOK
			}
			assert.Equal(t, expectedStatus, response.Code)
		})
	}
}

func setupCreemPaymentTest(t *testing.T) (*model.User, service.AuthIdentity) {
	t.Helper()
	user, identity := setupUserRewardsTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}))
	confirmPaymentComplianceForTest(t)
	key, products, secret, testMode := setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret, setting.CreemTestMode
	t.Cleanup(func() {
		setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret, setting.CreemTestMode = key, products, secret, testMode
	})
	setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret, setting.CreemTestMode = "test-key", "[]", "test-secret", true
	return user, identity
}

func sendCreemWebhookForTest(t *testing.T, event CreemWebhookEvent, validSignature bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(event)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/creem/webhook", bytes.NewReader(body))
	signature := "invalid"
	if validSignature {
		signature = generateCreemSignature(string(body), setting.CreemWebhookSecret)
	}
	ctx.Request.Header.Set(CreemSignatureHeader, signature)
	CreemWebhook(ctx)
	return response
}

func TestCreemPaidTopupDoesNotBindEmailOrConsumeFirstBindReward(t *testing.T) {
	for _, email := range []string{"", "verified@example.com"} {
		t.Run(email, func(t *testing.T) {
			user, identity := setupCreemPaymentTest(t)
			require.NoError(t, model.DB.Model(user).Update("email", email).Error)
			order := model.TopUp{UserId: user.Id, Amount: 12345, Money: 1, TradeNo: "creem-topup", PaymentMethod: model.PaymentMethodCreem, PaymentProvider: model.PaymentProviderCreem, Status: common.TopUpStatusPending}
			require.NoError(t, model.DB.Create(&order).Error)
			event := CreemWebhookEvent{EventType: "checkout.completed"}
			event.Object.RequestId, event.Object.Order.Status = order.TradeNo, "paid"
			event.Object.Customer.Email = "unverified-payment@example.com"
			setting.CreemApiKey = "" // Existing orders still settle after checkout is disabled.
			require.Equal(t, http.StatusUnauthorized, sendCreemWebhookForTest(t, event, false).Code)
			for range 2 {
				require.Equal(t, http.StatusOK, sendCreemWebhookForTest(t, event, true).Code)
			}
			// A different process can already have observed the pending row.
			// Model settlement must also acknowledge a duplicate without a new credit.
			require.NoError(t, model.RechargeCreem(order.TradeNo, "127.0.0.1"))
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, 12345, logs[0].Quota)
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.Equal(t, 12345, user.Quota, "duplicate callbacks must not credit twice")
			require.Equal(t, email, user.Email, "checkout email is not verified account identity")
			var grantCount int64
			require.NoError(t, model.DB.Model(&model.BonusQuotaGrant{}).Where("user_id = ?", user.Id).Count(&grantCount).Error)
			assert.Zero(t, grantCount)
			if email == "" {
				mailbox := newSecurityMailbox(t)
				flow := startSecurityEmailBinding(t, identity, "first@example.com", service.VerificationMethodPassword)
				state, err := service.FinishEmailBinding(identity, flow.FlowToken, mailbox.code(t, flow.Email), "")
				require.NoError(t, err)
				assert.Equal(t, 500, state.QuotaAwarded)
			}
		})
	}
}

type creemCheckoutTestTransport func(*http.Request) (*http.Response, error)

func (f creemCheckoutTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCreemTopupPreflightAndHiddenOnlineCheckout(t *testing.T) {
	user, identity := setupCreemPaymentTest(t)
	originalQuotaPerUnit, originalHidden := common.QuotaPerUnit, common.HideOnlineTopUp
	common.QuotaPerUnit, common.HideOnlineTopUp = 500000, true
	t.Cleanup(func() {
		common.QuotaPerUnit, common.HideOnlineTopUp = originalQuotaPerUnit, originalHidden
	})
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	checkoutCalls := 0
	http.DefaultTransport = creemCheckoutTestTransport(func(request *http.Request) (*http.Response, error) {
		checkoutCalls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"checkout_url":"https://checkout.example.test/topup"}`)), Header: make(http.Header)}, nil
	})
	for _, tc := range []struct {
		name, key, products, secret string
	}{
		{"missing webhook secret in test mode", "test-key", `[{"productId":"prod","quota":1}]`, ""},
		{"blank webhook secret in test mode", "test-key", `[{"productId":"prod","quota":1}]`, " "},
		{"missing API key", " ", `[{"productId":"prod","quota":1}]`, "test-secret"},
		{"no recharge catalog", "test-key", "[]", "test-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = tc.key, tc.products, tc.secret
			response := securityEnrollmentRequest(http.MethodPost, "/api/user/creem/pay", `{"product_id":"prod","payment_method":"creem"}`, "", identity, RequestCreemPay)
			assert.JSONEq(t, `{"message":"error","data":"Creem top-up is not available","message_key":"Creem top-up is not available"}`, response.Body.String())
			// The adaptor is another entry point and must enforce the same preflight.
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/creem/pay", nil)
			ctx.Set("id", user.Id)
			creemAdaptor.RequestPay(ctx, &CreemPayRequest{ProductId: "prod", PaymentMethod: "creem"})
			assert.JSONEq(t, response.Body.String(), recorder.Body.String())
			var count int64
			require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
			assert.Zero(t, count)
			assert.Zero(t, checkoutCalls)
		})
	}
	setting.CreemApiKey, setting.CreemWebhookSecret = "test-key", " "
	_, err := genCreemLink(context.Background(), "ref", &CreemProduct{ProductId: "prod"}, "", user.Username)
	require.EqualError(t, err, "Creem webhook is not configured")
	assert.Zero(t, checkoutCalls)

	setting.CreemApiKey, setting.CreemWebhookSecret = "test-key", "test-secret"
	setting.CreemProducts = `[{"productId":"prod","price":0.99,"quota":0.14}]`
	response := securityEnrollmentRequest(http.MethodPost, "/api/user/creem/pay", `{"product_id":"prod","payment_method":"creem"}`, "", identity, RequestCreemPay)
	require.Contains(t, response.Body.String(), `"message":"success"`)
	assert.Equal(t, 1, checkoutCalls)
	var order model.TopUp
	require.NoError(t, model.DB.Where("user_id = ?", user.Id).First(&order).Error)
	assert.EqualValues(t, 70000, order.Amount, "CREEM stores internal quota, not dollars")
	assert.Equal(t, 0.99, order.Money)
	for range 2 {
		require.NoError(t, model.ManualCompleteTopUp(order.TradeNo, "127.0.0.1"))
	}
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 70000, user.Quota, "manual settlement must not multiply CREEM quota again")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeTopup).Find(&logs).Error)
	require.Len(t, logs, 1, "duplicate manual settlement must not write a phantom credit log")
	assert.Equal(t, user.Id, logs[0].UserId)
	assert.Equal(t, 70000, logs[0].Quota)
}

func TestCreemSubscriptionCheckoutAndCallbackWithoutRechargeCatalog(t *testing.T) {
	user, identity := setupCreemPaymentTest(t)
	plan := model.SubscriptionPlan{Title: "CREEM subscription", Enabled: true, PriceAmount: 2, CreemProductId: "subscription-product", DurationUnit: "month", DurationValue: 1}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	checkoutCalls := 0
	http.DefaultTransport = creemCheckoutTestTransport(func(request *http.Request) (*http.Response, error) {
		checkoutCalls++
		var body CreemCheckoutRequest
		require.NoError(t, common.DecodeJson(request.Body, &body))
		assert.Equal(t, plan.CreemProductId, body.ProductId)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"checkout_url":"https://checkout.example.test/subscription"}`)), Header: make(http.Header)}, nil
	})
	for _, scenario := range []string{"unconfirmed", "old terms", "missing key", "missing secret in test mode", "missing plan product"} {
		t.Run(scenario, func(t *testing.T) {
			payment := operation_setting.GetPaymentSetting()
			payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
			setting.CreemApiKey, setting.CreemWebhookSecret = "test-key", "test-secret"
			product := "subscription-product"
			switch scenario {
			case "unconfirmed":
				payment.ComplianceConfirmed = false
			case "old terms":
				payment.ComplianceTermsVersion = "old-terms"
			case "missing key":
				setting.CreemApiKey = " "
			case "missing secret in test mode":
				setting.CreemWebhookSecret = ""
			case "missing plan product":
				product = " "
			}
			require.NoError(t, model.DB.Model(&plan).Update("creem_product_id", product).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			response := securityEnrollmentRequest(http.MethodPost, "/api/subscription/creem/pay", fmt.Sprintf(`{"plan_id":%d}`, plan.Id), "", identity, SubscriptionRequestCreemPay)
			assert.NotContains(t, response.Body.String(), `"message":"success"`)
			assert.Zero(t, checkoutCalls)
			if scenario == "unconfirmed" || scenario == "old terms" {
				response = securityEnrollmentRequest(http.MethodPost, "/api/user/creem/pay", `{}`, "", identity, RequestCreemPay)
				assert.Contains(t, response.Body.String(), `"success":false`)
			}
			var count int64
			require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&count).Error)
			assert.Zero(t, count, "configuration rejection must not create a pending order")
		})
	}
	setting.CreemApiKey, setting.CreemWebhookSecret = "test-key", "test-secret"
	require.NoError(t, model.DB.Model(&plan).Update("creem_product_id", "subscription-product").Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	response := securityEnrollmentRequest(http.MethodPost, "/api/subscription/creem/pay", fmt.Sprintf(`{"plan_id":%d}`, plan.Id), "", identity, SubscriptionRequestCreemPay)
	require.Contains(t, response.Body.String(), `"message":"success"`)
	require.Equal(t, 1, checkoutCalls)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.Where("user_id = ?", user.Id).First(&order).Error)
	event := CreemWebhookEvent{EventType: "checkout.completed"}
	event.Object.RequestId, event.Object.Order.Status = order.TradeNo, "paid"
	for range 2 {
		require.Equal(t, http.StatusOK, sendCreemWebhookForTest(t, event, true).Code)
	}
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota, "subscription completion must not top up the wallet")
}

func TestSubscriptionBalanceDecimalCostMatchesCheckout(t *testing.T) {
	for _, quota := range []int{6, 7} {
		t.Run(fmt.Sprint(quota), func(t *testing.T) {
			user, identity := setupCreemPaymentTest(t)
			originalUnit := common.QuotaPerUnit
			common.QuotaPerUnit = 100
			t.Cleanup(func() { common.QuotaPerUnit = originalUnit })
			require.NoError(t, model.DB.Model(user).Update("quota", quota).Error)
			_, err := model.CreateBonusQuotaGrant(user.Id, model.BonusQuotaSourceEmailBind, "", 500, 0)
			require.NoError(t, err)
			plan := model.SubscriptionPlan{Title: "Decimal balance plan", Enabled: true, PriceAmount: 0.07, DurationUnit: "month", DurationValue: 1}
			require.NoError(t, model.DB.Create(&plan).Error)
			response := securityEnrollmentRequest(http.MethodPost, "/api/subscription/balance/pay", fmt.Sprintf(`{"plan_id":%d}`, plan.Id), "", identity, SubscriptionRequestBalancePay)
			var result struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.Equal(t, quota == 7, result.Success, response.Body.String())
			require.NoError(t, model.DB.First(user, user.Id).Error)
			var count int64
			require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
			if quota == 7 {
				assert.Zero(t, user.Quota)
				assert.EqualValues(t, 1, count)
			} else {
				assert.Equal(t, quota, user.Quota)
				assert.Zero(t, count)
			}
			bonus, err := model.GetUserBonusQuotaTotal(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 500, bonus, "subscription purchases cannot spend restricted rewards")
		})
	}
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = ""
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = true
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = ""
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeProductID = originalProductID
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	setting.WaffoPancakeMerchantID = ""
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = "product"
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeMerchantID = "merchant"
	require.True(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = "product"
	setting.WaffoPancakePrivateKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func TestIPayNowTopUpEnabledRequiresComplianceAndConfig(t *testing.T) {
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	originalAppID := operation_setting.IPayNowAppId
	originalAppKey := operation_setting.IPayNowAppKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
		operation_setting.IPayNowAppId = originalAppID
		operation_setting.IPayNowAppKey = originalAppKey
	})

	operation_setting.IPayNowAppId = "app_id"
	operation_setting.IPayNowAppKey = "app_key"
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = ""
	require.False(t, isIPayNowTopUpEnabled())

	confirmPaymentComplianceForTest(t)
	require.True(t, isIPayNowTopUpEnabled())

	operation_setting.IPayNowAppKey = ""
	require.False(t, isIPayNowTopUpEnabled())
}

func TestIPayNowWebhookEnabledFollowsTopUpEnabled(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAppID := operation_setting.IPayNowAppId
	originalAppKey := operation_setting.IPayNowAppKey
	t.Cleanup(func() {
		operation_setting.IPayNowAppId = originalAppID
		operation_setting.IPayNowAppKey = originalAppKey
	})

	operation_setting.IPayNowAppId = ""
	operation_setting.IPayNowAppKey = "app_key"
	require.False(t, isIPayNowWebhookEnabled())

	operation_setting.IPayNowAppId = "app_id"
	require.True(t, isIPayNowWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayWebhookEnabled())

	operation_setting.EpayKey = "epay_key"
	require.True(t, isEpayWebhookEnabled())

	operation_setting.PayMethods = nil
	require.False(t, isEpayWebhookEnabled())
}

func TestEpayCallbacksVerifyBeforeSettlement(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	originalServerAddress := system_setting.ServerAddress
	originalDB := model.DB
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
		system_setting.ServerAddress = originalServerAddress
		model.DB = originalDB
	})
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	system_setting.ServerAddress = "https://dashboard.example.com"
	// Rejected callbacks and valid non-success events must never reach settlement.
	// A nil database makes an accidental attempt to settle fail the test.
	model.DB = nil
	require.True(t, isEpayWebhookEnabled())

	// Both ambiguous maps below had this same valid signing string in v0.0.4.
	// This must fail because of parameter ambiguity, not a mismatched signature.
	ambiguousSign := epay.MD5String("money=1.00&name=item&out_trade_no=other&out_trade_no=order&pid=epay_id&trade_status=TRADE_SUCCESS&type=alipay", "test-key")
	pendingSign := epay.MD5String("money=1.00&name=item%26other&out_trade_no=order&pid=epay_id&trade_status=WAIT_BUYER_PAY&type=alipay", "test-key")
	cases := []struct {
		name         string
		productName  string
		tradeNo      string
		status       string
		sign         string
		validPending bool
	}{
		{"separator in product name", "item&out_trade_no=other", "order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"separator in order number", "item", "other&out_trade_no=order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"invalid signature", "item", "order", epay.StatusTradeSuccess, "invalid", false},
		{"missing signature", "item", "order", epay.StatusTradeSuccess, "", false},
		{"valid pending with literal percent escape", "item%26other", "order", "WAIT_BUYER_PAY", pendingSign, true},
	}
	endpoints := []struct {
		name             string
		path             string
		handler          gin.HandlerFunc
		status           int
		rejectedBody     string
		pendingBody      string
		rejectedLocation string
		pendingLocation  string
	}{
		{
			name: "topup notify", path: "/api/user/epay/notify", handler: EpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "success",
		},
		{
			name: "subscription notify", path: "/api/subscription/epay/notify", handler: SubscriptionEpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "fail",
		},
		{
			name: "subscription return", path: "/api/subscription/epay/return", handler: SubscriptionEpayReturn,
			status:           http.StatusFound,
			rejectedLocation: "https://dashboard.example.com/wallet?pay=fail",
			pendingLocation:  "https://dashboard.example.com/wallet?pay=pending",
		},
	}
	router := gin.New()
	for _, endpoint := range endpoints {
		router.Any(endpoint.path, endpoint.handler)
	}
	for _, endpoint := range endpoints {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, tc := range cases {
				t.Run(endpoint.name+"/"+method+"/"+tc.name, func(t *testing.T) {
					params := url.Values{
						"pid": {"epay_id"}, "type": {"alipay"}, "money": {"1.00"},
						"name": {tc.productName}, "out_trade_no": {tc.tradeNo},
						"trade_status": {tc.status}, "sign_type": {"MD5"},
					}
					if tc.sign != "" {
						params.Set("sign", tc.sign)
					}
					request := httptest.NewRequest(method, endpoint.path+"?"+params.Encode(), nil)
					if method == http.MethodPost {
						request = httptest.NewRequest(method, endpoint.path, strings.NewReader(params.Encode()))
						request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					recorder := httptest.NewRecorder()
					require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
					assert.Equal(t, endpoint.status, recorder.Code)
					wantBody, wantLocation := endpoint.rejectedBody, endpoint.rejectedLocation
					if tc.validPending {
						wantBody, wantLocation = endpoint.pendingBody, endpoint.pendingLocation
					}
					if wantBody != "" {
						assert.Equal(t, wantBody, recorder.Body.String())
					}
					assert.Equal(t, wantLocation, recorder.Header().Get("Location"))
				})
			}
		}
	}
}
