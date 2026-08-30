package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTaskPersistsSelectedTMLabKeyForPolling(t *testing.T) {
	task := InitTask(constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeTMLabSeedance)), &relaycommon.RelayInfo{
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
