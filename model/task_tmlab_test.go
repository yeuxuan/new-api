package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTaskPersistsSelectedTMLabKeyForPolling(t *testing.T) {
	task := InitTask(constant.TaskPlatform("61"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeTMLabSeedance,
			ApiKey:      "selected-key",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})

	assert.Equal(t, "selected-key", task.PrivateData.Key)
}

func TestTaskPrivateDataPersistsTieredBillingSnapshot(t *testing.T) {
	original := TaskPrivateData{
		BillingContext: &TaskBillingContext{
			OriginModelName: "seedance-2.0-fast",
			PerCallBilling:  true,
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:              "tiered_expr",
				ModelName:                "seedance-2.0-fast",
				ExprString:               `tier("base", param("duration") * 0.7)`,
				EstimatedQuotaAfterGroup: 191781,
				EstimatedTier:            "base",
			},
		},
	}

	value, err := original.Value()
	require.NoError(t, err)
	var restored TaskPrivateData
	require.NoError(t, restored.Scan(value))

	require.NotNil(t, restored.BillingContext)
	require.NotNil(t, restored.BillingContext.TieredBillingSnapshot)
	assert.True(t, restored.BillingContext.PerCallBilling)
	assert.Equal(t, 191781, restored.BillingContext.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
	assert.Equal(t, "base", restored.BillingContext.TieredBillingSnapshot.EstimatedTier)
}

func TestNativeTaskPathFiltersNonTMLabChannels(t *testing.T) {
	t.Run("memory cache", func(t *testing.T) {
		previousChannels := channelsIDM
		channelsIDM = map[int]*Channel{
			1: {Id: 1, Type: constant.ChannelTypeOpenAI},
			2: {Id: 2, Type: constant.ChannelTypeTMLabSeedance},
		}
		t.Cleanup(func() { channelsIDM = previousChannels })

		assert.Equal(t, []int{2}, filterChannelsByRequestPathAndModel([]int{1, 2, 999}, "/v1/tasks", "seedance-2.0-fast"))
	})

	t.Run("database", func(t *testing.T) {
		truncateTables(t)
		require.NoError(t, DB.Create(&Channel{Id: 11, Type: constant.ChannelTypeOpenAI}).Error)
		require.NoError(t, DB.Create(&Channel{Id: 12, Type: constant.ChannelTypeTMLabSeedance}).Error)
		abilities := []Ability{
			{ChannelId: 11, Model: "seedance-2.0-fast", Group: "default", Enabled: true},
			{ChannelId: 12, Model: "seedance-2.0-fast", Group: "default", Enabled: true},
		}

		filtered := filterAbilitiesByRequestPathAndModel(abilities, "/v1/tasks", "seedance-2.0-fast")
		require.Len(t, filtered, 1)
		assert.Equal(t, 12, filtered[0].ChannelId)
	})
}
