package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetPricingEndpointTestTables(t *testing.T) {
	t.Helper()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))
	for _, table := range []string{"abilities", "channels", "models", "vendors"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	InitChannelCache()
	InvalidatePricingCache()
	t.Cleanup(func() {
		for _, table := range []string{"abilities", "channels", "models", "vendors"} {
			require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
		}
		InitChannelCache()
		InvalidatePricingCache()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
}

func insertPricingEndpointChannel(t *testing.T, channelID int, channelType int, settings dto.ChannelOtherSettings) {
	t.Helper()
	channel := &Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: common.ChannelStatusEnabled,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}
	if settings.AdvancedCustom != nil {
		channel.SetOtherSettings(settings)
	}
	require.NoError(t, DB.Create(channel).Error)
}

func insertPricingEndpointAbility(t *testing.T, channelID int, modelName string) {
	t.Helper()
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func pricingEndpointAdvancedCustomConfig(routes ...dto.AdvancedCustomRoute) dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: routes,
		},
	}
}

func pricingEndpointTypesByModel(t *testing.T) map[string][]constant.EndpointType {
	t.Helper()
	InitChannelCache()
	return pricingEndpointTypesFromPricing(GetPricing())
}

func pricingEndpointTypesFromPricing(pricings []Pricing) map[string][]constant.EndpointType {
	byModel := make(map[string][]constant.EndpointType)
	for _, pricing := range pricings {
		byModel[pricing.ModelName] = pricing.SupportedEndpointTypes
	}
	return byModel
}

func TestPricingAdvancedCustomUsesConfiguredEndpointTypes(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 101, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 101, "gemini-2.5-flash")
	insertPricingEndpointAbility(t, 101, "gpt-4o")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, byModel["gemini-2.5-flash"])
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
	}, byModel["gpt-4o"])
}

func TestPricingModelMetadataEndpointsMergeWithAdvancedCustomInference(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 103, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 103, "gemini-2.5-flash")
	require.NoError(t, DB.Create(&Model{
		ModelName: "gemini-2.5-flash",
		Endpoints: `{
			"openai": "/v1/chat/completions"
		}`,
		Status:   1,
		NameRule: NameRuleExact,
	}).Error)

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAI,
	}, byModel["gemini-2.5-flash"])
}

func TestPricingModelMetadataEndpointsCanProvideEndpointWithoutChannelInference(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 104, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 104, "metadata-only-model")
	require.NoError(t, DB.Create(&Model{
		ModelName: "metadata-only-model",
		Endpoints: `{
			"openai": "/v1/chat/completions"
		}`,
		Status:   1,
		NameRule: NameRuleExact,
	}).Error)

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["metadata-only-model"])
}

func TestGetModelAPIDocumentReturnsEnabledModelDocumentation(t *testing.T) {
	resetPricingEndpointTestTables(t)

	const apiDocument = "# Documented model\n\nUse `POST /v1/tasks` to create a task."
	require.NoError(t, DB.Create(&Model{
		ModelName:   "documented-model",
		APIDocument: apiDocument,
		Status:      1,
		NameRule:    NameRuleExact,
	}).Error)

	storedDocument, err := GetModelAPIDocument("documented-model")
	require.NoError(t, err)
	assert.Equal(t, apiDocument, storedDocument)

	missingDocument, err := GetModelAPIDocument("missing-model")
	require.NoError(t, err)
	assert.Empty(t, missingDocument)
}

func TestGetModelAPIDocumentUsesMostSpecificMatchingRule(t *testing.T) {
	resetPricingEndpointTestTables(t)

	for _, modelMeta := range []Model{
		{ModelName: "seedance-", APIDocument: "prefix", Status: 1, NameRule: NameRulePrefix},
		{ModelName: "seedance-2.0-", APIDocument: "specific-prefix", Status: 1, NameRule: NameRulePrefix},
		{ModelName: "-pro", APIDocument: "suffix", Status: 1, NameRule: NameRuleSuffix},
	} {
		require.NoError(t, DB.Create(&modelMeta).Error)
	}

	apiDocument, err := GetModelAPIDocument("seedance-2.0-pro")
	require.NoError(t, err)
	assert.Equal(t, "specific-prefix", apiDocument)
}

func TestPricingMetadataAndAPIDocumentUseSameMatchingRule(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 105, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 105, "seedance-2.0-pro")
	for _, modelMeta := range []Model{
		{
			ModelName:   "seedance-",
			Description: "broad description",
			APIDocument: "broad document",
			Status:      1,
			NameRule:    NameRulePrefix,
		},
		{
			ModelName:   "seedance-2.0-",
			Description: "specific description",
			APIDocument: "specific document",
			Status:      1,
			NameRule:    NameRulePrefix,
		},
	} {
		require.NoError(t, DB.Create(&modelMeta).Error)
	}

	InitChannelCache()
	var pricingModel *Pricing
	for _, pricing := range GetPricing() {
		if pricing.ModelName == "seedance-2.0-pro" {
			pricingCopy := pricing
			pricingModel = &pricingCopy
			break
		}
	}
	require.NotNil(t, pricingModel)
	assert.Equal(t, "specific description", pricingModel.Description)

	apiDocument, err := GetModelAPIDocument("seedance-2.0-pro")
	require.NoError(t, err)
	assert.Equal(t, "specific document", apiDocument)
}

func TestGetModelAPIDocumentDoesNotBypassExactDisabledModel(t *testing.T) {
	resetPricingEndpointTestTables(t)

	require.NoError(t, DB.Create(&Model{
		ModelName:   "seedance-",
		APIDocument: "prefix",
		Status:      1,
		NameRule:    NameRulePrefix,
	}).Error)
	disabledModel := Model{
		ModelName:   "seedance-disabled",
		APIDocument: "exact",
		Status:      0,
		NameRule:    NameRuleExact,
	}
	require.NoError(t, disabledModel.Insert())

	apiDocument, err := GetModelAPIDocument("seedance-disabled")
	require.NoError(t, err)
	assert.Empty(t, apiDocument)
}

func TestModelAPIDocumentRejectsContentBeyondPortableTextLimit(t *testing.T) {
	resetPricingEndpointTestTables(t)

	modelMeta := &Model{
		ModelName:   "oversized-document-model",
		APIDocument: strings.Repeat("a", MaxModelAPIDocumentBytes+1),
		Status:      1,
		NameRule:    NameRuleExact,
	}
	require.Error(t, modelMeta.Insert())

	modelMeta.APIDocument = "# Valid documentation"
	require.NoError(t, modelMeta.Insert())
	modelMeta.APIDocument = strings.Repeat("b", MaxModelAPIDocumentBytes+1)
	require.Error(t, modelMeta.Update())

	var stored Model
	require.NoError(t, DB.First(&stored, modelMeta.Id).Error)
	assert.Equal(t, "# Valid documentation", stored.APIDocument)
}

func TestModelUpdateCanReplaceAndClearAPIDocument(t *testing.T) {
	resetPricingEndpointTestTables(t)

	modelMeta := &Model{
		ModelName:   "editable-document-model",
		APIDocument: "# Old documentation",
		Status:      1,
		NameRule:    NameRuleExact,
	}
	require.NoError(t, modelMeta.Insert())

	modelMeta.APIDocument = "# Updated documentation"
	require.NoError(t, modelMeta.Update())

	var stored Model
	require.NoError(t, DB.First(&stored, modelMeta.Id).Error)
	assert.Equal(t, "# Updated documentation", stored.APIDocument)

	modelMeta.APIDocument = ""
	require.NoError(t, modelMeta.Update())
	require.NoError(t, DB.First(&stored, modelMeta.Id).Error)
	assert.Empty(t, stored.APIDocument)
}

func TestModelUpdateWithoutAPIDocumentPreservesStoredDocument(t *testing.T) {
	resetPricingEndpointTestTables(t)

	modelMeta := &Model{
		ModelName:   "backward-compatible-model",
		Description: "old description",
		APIDocument: "# Existing documentation",
		Status:      1,
		NameRule:    NameRuleExact,
	}
	require.NoError(t, modelMeta.Insert())

	modelMeta.Description = "new description"
	modelMeta.APIDocument = ""
	require.NoError(t, modelMeta.UpdateWithoutAPIDocument())

	var stored Model
	require.NoError(t, DB.First(&stored, modelMeta.Id).Error)
	assert.Equal(t, "new description", stored.Description)
	assert.Equal(t, "# Existing documentation", stored.APIDocument)
}

func TestPricingAdvancedCustomMissingConfigFallsBackToChannelType(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 102, constant.ChannelTypeAdvancedCustom, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 102, "gpt-4o")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["gpt-4o"])
}

func TestPricingNativeChannelEndpointTypesUnchanged(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 201, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointChannel(t, 202, constant.ChannelTypeGemini, dto.ChannelOtherSettings{})
	insertPricingEndpointChannel(t, 203, constant.ChannelTypeAnthropic, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 201, "gpt-4o")
	insertPricingEndpointAbility(t, 202, "gemini-2.5-flash")
	insertPricingEndpointAbility(t, 203, "claude-3-5-sonnet")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["gpt-4o"])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}, byModel["gemini-2.5-flash"])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}, byModel["claude-3-5-sonnet"])
}

func TestInitChannelCacheInvalidatesPricingCache(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 301, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
	))
	insertPricingEndpointAbility(t, 301, "gemini-3.5-flash")
	InitChannelCache()

	initial := pricingEndpointTypesByModel(t)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, initial["gemini-3.5-flash"])

	var channel Channel
	require.NoError(t, DB.First(&channel, "id = ?", 301).Error)
	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", 301).Update("settings", channel.OtherSettings).Error)
	InitChannelCache()

	updated := pricingEndpointTypesByModel(t)
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, updated["gemini-3.5-flash"])
}

func TestInitChannelCacheInvalidatesStartupPricingBuiltBeforeChannelCache(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 302, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 302, "gemini-3.5-flash")

	staleByModel := pricingEndpointTypesFromPricing(GetPricing())
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, staleByModel["gemini-3.5-flash"])

	InitChannelCache()

	rebuiltByModel := pricingEndpointTypesFromPricing(GetPricing())
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, rebuiltByModel["gemini-3.5-flash"])
}

func TestCacheUpdateChannelSyncsAdvancedCustomConfig(t *testing.T) {
	resetPricingEndpointTestTables(t)

	channel := &Channel{
		Id:     401,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "key-401",
		Status: common.ChannelStatusEnabled,
		Name:   "channel-401",
	}
	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    "openai_responses_to_gemini_generate_content",
	}))
	CacheUpdateChannel(channel)

	require.NotNil(t, channel2advancedCustomConfig[401])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, channel2advancedCustomConfig[401].SupportedEndpointTypesForModel("gemini-3.5-flash"))

	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
	}))
	CacheUpdateChannel(channel)

	require.NotNil(t, channel2advancedCustomConfig[401])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, channel2advancedCustomConfig[401].SupportedEndpointTypesForModel("gemini-3.5-flash"))

	channel.Type = constant.ChannelTypeOpenAI
	CacheUpdateChannel(channel)

	assert.Nil(t, channel2advancedCustomConfig[401])
}
