package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskTieredBillingStub struct{}

func (taskTieredBillingStub) Settle(int) error              { return nil }
func (taskTieredBillingStub) Refund(*gin.Context)           {}
func (taskTieredBillingStub) NeedsRefund() bool             { return false }
func (taskTieredBillingStub) GetPreConsumedQuota() int      { return 0 }
func (taskTieredBillingStub) Reserve(int) error             { return nil }
func (taskTieredBillingStub) ReserveBeforeSubmit(int) error { return nil }

func TestRelayTaskSubmitTieredExprDoesNotApplyLegacyRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"seedance-2.0-fast":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"seedance-2.0-fast":"tier(\"base\", (param(\"duration\") == nil ? 0 : param(\"duration\")) * (param(\"resolution\") == \"720p\" ? 0.7 : 0.35) / 7.3 * 1000000)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))

	var submittedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		submittedBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{"task_id":"upstream_task_1","status":"queued"}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()

	body := `{"model":"seedance-2.0-fast","prompt":"cinematic forest","duration":4,"resolution":"720p"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeTMLabSeedance)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 64)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "upstream-test-key")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "seedance-2.0-fast")

	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-fast",
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		Billing:         taskTieredBillingStub{},
	}

	result, taskErr := RelayTaskSubmit(ctx, info)

	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, 191781, result.Quota)
	assert.Equal(t, result.Quota, info.PriceData.Quota)
	assert.Nil(t, info.PriceData.OtherRatios())
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.False(t, info.TieredBillingSnapshot.TaskUsageBilling, "native TMLab keeps its stored expression units")
	assert.Equal(t, "base", info.TieredBillingSnapshot.EstimatedTier)
	assert.Contains(t, submittedBody, `"duration":4`)
	assert.Contains(t, submittedBody, `"resolution":"720p"`)
}

func TestRelayTaskSubmitPluginConstantsUseUSD(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream_task_1"}`))
	}))
	t.Cleanup(upstream.Close)

	const flatExpr = `tier("flat", 0.25)`
	const freeExpr = `tier("free", 0)`
	const legacyExpr = `tier("legacy", 250000)`
	for _, tc := range []struct {
		name, model, mapping, mode, modelExpr, overrideModel, overrideExpr string
		wantExpr                                                           string
		wantCost                                                           float64
	}{
		{"provider override ignores ratio mode", "declared-model", "", "ratio", "", "declared-model", flatExpr, flatExpr, 0.25},
		{"mapped override beats model expression", "alias-model", `{"alias-model":"declared-model"}`, "tiered_expr", `tier("model", 1)`, "declared-model", flatExpr, flatExpr, 0.25},
		{"zero provider override stays free", "declared-model", "", "ratio", "", "declared-model", freeExpr, freeExpr, 0},
		{"inherited model expression keeps historical units", "declared-model", "", "tiered_expr", legacyExpr, "", "", legacyExpr, 0.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			modes, err := common.Marshal(map[string]string{tc.model: tc.mode})
			require.NoError(t, err)
			expressions, err := common.Marshal(map[string]string{tc.model: tc.modelExpr})
			require.NoError(t, err)
			overrides := map[string]string{}
			if tc.overrideModel != "" {
				overrides[billing_setting.PluginBillingExprKey("bill-fallback", tc.overrideModel)] = tc.overrideExpr
			}
			variants, err := common.Marshal(overrides)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode":          string(modes),
				"billing_setting.billing_expr":          string(expressions),
				billing_setting.PluginBillingExprOption: string(variants),
				"group_ratio_setting.group_ratio":       `{"default":2}`,
				"group_ratio_setting.group_group_ratio": `{}`,
			}))

			c, info := newTaskSubmitContext(t, tc.model, tc.mapping)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			c.Set("group", "default")
			info.UserGroup, info.UsingGroup = "default", "default"
			info.OriginModelName = tc.model
			info.Billing = taskTieredBillingStub{}
			pinMappingOrderPlugin(t, c, billingFallbackPlugin)

			result, taskErr := RelayTaskSubmit(c, info)
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			snap := info.TieredBillingSnapshot
			require.NotNil(t, snap)
			assert.Equal(t, tc.overrideModel != "", snap.TaskUsageBilling)
			assert.Equal(t, tc.wantExpr, snap.ExprString)
			assert.Equal(t, tc.wantCost*common.QuotaPerUnit, snap.EstimatedQuotaBeforeGroup)
			wantQuota := common.QuotaRound(tc.wantCost * common.QuotaPerUnit * 2)
			assert.Equal(t, wantQuota, result.Quota)
			assert.Equal(t, wantQuota, info.PriceData.QuotaToPreConsume)
			assert.Nil(t, info.PriceData.OtherRatios())

			// Completion must retain both the expression and its USD conversion
			// after live provider/model pricing changes.
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: `{}`,
				"billing_setting.billing_expr":          `{}`,
			}))
			settled, _, err := service.EvaluateTaskCompletionUsage(snap, nil)
			require.NoError(t, err)
			assert.Equal(t, wantQuota, settled.ActualQuotaAfterGroup)
			assert.Equal(t, tc.wantCost*common.QuotaPerUnit, settled.ActualQuotaBeforeGroup)
			assert.Equal(t, billingexpr.ExprHashString(tc.wantExpr), snap.ExprHash)
		})
	}
}
