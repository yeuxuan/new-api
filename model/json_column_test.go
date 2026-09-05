package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONColumnValueUsesPlainText(t *testing.T) {
	payload, err := common.Marshal(map[string]int{"0": 1})
	require.NoError(t, err)

	value, err := jsonColumnValue(payload, nil)
	require.NoError(t, err)
	text, ok := value.(string)
	require.True(t, ok, "json/jsonb binds must be string, not []byte")
	assert.JSONEq(t, `{"0":1}`, text)
}

func TestChannelInfoValueAndScanRoundTrip(t *testing.T) {
	info := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
		MultiKeyPollingIndex: 1,
		MultiKeyMode:         constant.MultiKeyModePolling,
	}

	value, err := info.Value()
	require.NoError(t, err)
	text, ok := value.(string)
	require.True(t, ok, "channel_info must bind as JSON text for PostgreSQL simple protocol")
	require.True(t, common.GetJsonType([]byte(text)) == "object")

	var fromString ChannelInfo
	require.NoError(t, fromString.Scan(text))
	assert.Equal(t, info.IsMultiKey, fromString.IsMultiKey)
	assert.Equal(t, info.MultiKeySize, fromString.MultiKeySize)
	assert.Equal(t, info.MultiKeyStatusList[0], fromString.MultiKeyStatusList[0])
	assert.Equal(t, info.MultiKeyPollingIndex, fromString.MultiKeyPollingIndex)
	assert.Equal(t, info.MultiKeyMode, fromString.MultiKeyMode)

	var fromBytes ChannelInfo
	require.NoError(t, fromBytes.Scan([]byte(text)))
	assert.Equal(t, fromString, fromBytes)
}

func TestJSONValueBindsAsPlainText(t *testing.T) {
	value, err := JSONValue(`["gpt-4o"]`).Value()
	require.NoError(t, err)
	text, ok := value.(string)
	require.True(t, ok)
	assert.JSONEq(t, `["gpt-4o"]`, text)
}
