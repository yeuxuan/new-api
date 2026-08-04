package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskTieredBillingStub struct{}

func (taskTieredBillingStub) Settle(int) error         { return nil }
func (taskTieredBillingStub) Refund(*gin.Context)      {}
func (taskTieredBillingStub) NeedsRefund() bool        { return false }
func (taskTieredBillingStub) GetPreConsumedQuota() int { return 0 }
func (taskTieredBillingStub) Reserve(int) error        { return nil }

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
	assert.Equal(t, "base", info.TieredBillingSnapshot.EstimatedTier)
	assert.Contains(t, submittedBody, `"duration":4`)
	assert.Contains(t, submittedBody, `"resolution":"720p"`)
}
