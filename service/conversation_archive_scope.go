package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ShouldArchiveConversation reports whether a relay request belongs to a
// conversational protocol. Media generation and non-conversational utility
// endpoints are intentionally excluded from the training archive.
func ShouldArchiveConversation(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		return true
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return true
	case types.RelayFormatOpenAIRealtime:
		return true
	case types.RelayFormatGemini:
		return isGeminiConversation(info)
	case types.RelayFormatOpenAI:
		if openAIRequestGeneratesAudio(info.Request) {
			return false
		}
		switch info.RelayMode {
		case relayconstant.RelayModeChatCompletions, relayconstant.RelayModeCompletions:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func isGeminiConversation(info *relaycommon.RelayInfo) bool {
	path := strings.ToLower(strings.SplitN(info.RequestURLPath, "?", 2)[0])
	if !strings.HasSuffix(path, ":generatecontent") && !strings.HasSuffix(path, ":streamgeneratecontent") {
		return false
	}

	request, ok := info.Request.(*dto.GeminiChatRequest)
	if !ok || request == nil {
		return true
	}
	if len(request.GenerationConfig.ImageConfig) > 0 || len(request.GenerationConfig.SpeechConfig) > 0 {
		return false
	}
	for _, modality := range request.GenerationConfig.ResponseModalities {
		switch strings.ToLower(strings.TrimSpace(modality)) {
		case "audio", "image", "video":
			return false
		}
	}
	return true
}

func openAIRequestGeneratesAudio(request dto.Request) bool {
	openAIRequest, ok := request.(*dto.GeneralOpenAIRequest)
	if !ok || openAIRequest == nil {
		return false
	}
	if len(openAIRequest.Audio) > 0 {
		return true
	}
	var modalities []string
	if err := common.Unmarshal(openAIRequest.Modalities, &modalities); err != nil {
		return false
	}
	for _, modality := range modalities {
		if strings.EqualFold(strings.TrimSpace(modality), "audio") {
			return true
		}
	}
	return false
}
