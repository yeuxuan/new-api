package service

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func TestShouldArchiveConversation(t *testing.T) {
	tests := []struct {
		name string
		info *relaycommon.RelayInfo
		want bool
	}{
		{
			name: "openai chat",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions},
			want: true,
		},
		{
			name: "openai legacy completion",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeCompletions},
			want: true,
		},
		{
			name: "openai audio chat output",
			info: &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAI,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				Request:     &dto.GeneralOpenAIRequest{Modalities: []byte(`["text","audio"]`)},
			},
			want: false,
		},
		{
			name: "responses",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses},
			want: true,
		},
		{
			name: "responses compaction",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponsesCompaction, RelayMode: relayconstant.RelayModeResponsesCompact},
			want: true,
		},
		{
			name: "openai realtime",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIRealtime, RelayMode: relayconstant.RelayModeRealtime},
			want: true,
		},
		{
			name: "claude messages",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude},
			want: true,
		},
		{
			name: "gemini generate content",
			info: &relaycommon.RelayInfo{
				RelayFormat:    types.RelayFormatGemini,
				RequestURLPath: "/v1beta/models/gemini:generateContent",
				Request:        &dto.GeminiChatRequest{},
			},
			want: true,
		},
		{
			name: "gemini image output",
			info: &relaycommon.RelayInfo{
				RelayFormat:    types.RelayFormatGemini,
				RequestURLPath: "/v1beta/models/gemini:generateContent",
				Request: &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{
					ResponseModalities: []string{"TEXT", "IMAGE"},
				}},
			},
			want: false,
		},
		{
			name: "gemini image config",
			info: &relaycommon.RelayInfo{
				RelayFormat:    types.RelayFormatGemini,
				RequestURLPath: "/v1beta/models/gemini:generateContent",
				Request: &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{
					ImageConfig: []byte(`{"aspectRatio":"1:1"}`),
				}},
			},
			want: false,
		},
		{
			name: "gemini speech config",
			info: &relaycommon.RelayInfo{
				RelayFormat:    types.RelayFormatGemini,
				RequestURLPath: "/v1beta/models/gemini:streamGenerateContent?alt=sse",
				Request: &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{
					SpeechConfig: []byte(`{"voiceConfig":{}}`),
				}},
			},
			want: false,
		},
		{
			name: "gemini action prefix is not accepted",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, RequestURLPath: "/v1beta/models/gemini:generateContentFoo"},
			want: false,
		},
		{
			name: "gemini embedding",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, RequestURLPath: "/v1beta/models/gemini:embedContent"},
			want: false,
		},
		{
			name: "image generation",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeImagesGenerations},
			want: false,
		},
		{
			name: "audio endpoint",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAudio, RelayMode: relayconstant.RelayModeAudioSpeech},
			want: false,
		},
		{
			name: "embedding",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatEmbedding, RelayMode: relayconstant.RelayModeEmbeddings},
			want: false,
		},
		{
			name: "rerank",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatRerank, RelayMode: relayconstant.RelayModeRerank},
			want: false,
		},
		{
			name: "moderation",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeModerations},
			want: false,
		},
		{
			name: "legacy edits",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeEdits},
			want: false,
		},
		{
			name: "alpha search",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAlphaSearch, RelayMode: relayconstant.RelayModeAlphaSearch},
			want: false,
		},
		{
			name: "async media task",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatTask},
			want: false,
		},
		{
			name: "midjourney",
			info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatMjProxy},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ShouldArchiveConversation(tt.info))
		})
	}
}
