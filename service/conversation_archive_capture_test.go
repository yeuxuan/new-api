package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/conversationarchive"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGeneratedMediaFilterRedactsResponsesAndKeepsText(t *testing.T) {
	payload := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"保留回答"}]},{"type":"image_generation_call","status":"completed","result":"QUJD"}]}`)
	filter := &generatedMediaFilter{}
	archived, count := filter.Append(payload, true)
	require.Equal(t, 1, count)
	assert.Contains(t, string(archived), "保留回答")
	assert.NotContains(t, string(archived), `"result":"QUJD"`)
	assert.Contains(t, string(archived), `"omitted":"generated_media"`)
}

func TestGeneratedMediaFilterRedactsMimeTypedProviderOutput(t *testing.T) {
	payload := []byte(`{"candidates":[{"content":{"parts":[{"text":"文字仍保留"},{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]}}]}`)
	filter := &generatedMediaFilter{}
	archived, count := filter.Append(payload, true)
	require.Equal(t, 1, count)
	assert.Contains(t, string(archived), "文字仍保留")
	assert.NotContains(t, string(archived), `"data":"QUJD"`)
	assert.Contains(t, string(archived), `"omitted":"generated_media"`)
}

func TestGeneratedMediaFilterBoundsLargeSplitSSEEvent(t *testing.T) {
	filter := &generatedMediaFilter{}
	large := strings.Repeat("QUJD", generatedMediaBufferLimit/4+100)
	first := []byte(`data: {"type":"response.image_generation_call.partial_image","partial_image_b64":"` + large)
	archived, count := filter.Append(first, false)
	assert.Empty(t, archived)
	assert.Zero(t, count)

	archived, count = filter.Append([]byte(`"}`+"\r\n\r\n"), false)
	require.Equal(t, 1, count)
	assert.NotContains(t, string(archived), large[:100])
	assert.Contains(t, string(archived), "generated_media_event")

	text, count := filter.Append([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"后续文本\"}\n\n"), true)
	assert.Zero(t, count)
	assert.Contains(t, string(text), "后续文本")
}

func TestDetectsOnlyUnpinnedRemoteAttachments(t *testing.T) {
	assert.True(t, hasUnpinnedConversationAttachment([]byte(`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`)))
	assert.False(t, hasUnpinnedConversationAttachment([]byte(`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`)))
	assert.False(t, hasUnpinnedConversationAttachment([]byte(`{"callback_url":"https://example.com/hook"}`)))
}

func TestConversationArchiveRecordIDIsSafeAndStable(t *testing.T) {
	assert.Equal(t, "abc-123", conversationArchiveRecordID("req_abc-123"))
	longID := "req_" + strings.Repeat("用户/", 80)
	first := conversationArchiveRecordID(longID)
	second := conversationArchiveRecordID(longID)
	assert.Equal(t, first, second)
	assert.Len(t, first, 64)
	assert.Regexp(t, `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, first)
}

func TestBeginConversationCapturePersistsRecoveryIdentityImmediately(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	store, err := conversationarchive.New(root, conversationarchive.Options{})
	require.NoError(t, err)
	previousEnabled := common.ConversationLogEnabled
	previousPath := common.ConversationLogStoragePath
	conversationStoreMu.Lock()
	previousStore := conversationStore
	conversationStore = store
	conversationStoreMu.Unlock()
	common.ConversationLogEnabled = true
	common.ConversationLogStoragePath = root
	t.Cleanup(func() {
		common.ConversationLogEnabled = previousEnabled
		common.ConversationLogStoragePath = previousPath
		conversationStoreMu.Lock()
		conversationStore = previousStore
		conversationStoreMu.Unlock()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?api-version=2026-01-01&key=client-secret", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	ctx.Request.Header.Add("Anthropic-Beta", "context-1m-2025-08-07")
	ctx.Set("username", "recovery-user")
	ctx.Set("token_name", "training-token")
	startedAt := time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC)
	capture, err := BeginConversationCapture(ctx, &relaycommon.RelayInfo{
		RequestId:       "req_recovery-identity",
		UserId:          17,
		TokenId:         23,
		OriginModelName: "training-model",
		RelayFormat:     types.RelayFormatOpenAI,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		StartTime:       startedAt,
	})
	require.NoError(t, err)
	require.NotNil(t, capture)

	pendingPaths, err := store.PendingPaths()
	require.NoError(t, err)
	require.Len(t, pendingPaths, 1)
	descriptorData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pendingPaths[0]), "pending.json"))
	require.NoError(t, err)
	var descriptor struct {
		Metadata map[string]string `json:"metadata"`
	}
	require.NoError(t, common.Unmarshal(descriptorData, &descriptor))
	assert.Equal(t, "req_recovery-identity", descriptor.Metadata["request_id"])
	assert.Equal(t, "17", descriptor.Metadata["user_id"])
	assert.Equal(t, "recovery-user", descriptor.Metadata["username"])
	assert.Equal(t, "training-model", descriptor.Metadata["origin_model"])
	clientURL, err := url.Parse(descriptor.Metadata["client_url"])
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01", clientURL.Query().Get("api-version"))
	assert.Equal(t, "***masked***", clientURL.Query().Get("key"))
	assert.NotContains(t, descriptor.Metadata["client_url"], "client-secret")
	assert.Equal(t, `["context-1m-2025-08-07"]`, descriptor.Metadata["client_header_anthropic_beta"])
	require.NoError(t, capture.pending.Preserve(capture.metadata, []string{"test_cleanup"}))
}

func TestAuxiliaryProviderOperationsAreCapturedSeparately(t *testing.T) {
	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	pending, err := store.BeginPending(conversationarchive.Record{ID: "aux-1", RecordedAt: recordedAt, Protocol: "openai"})
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RetryIndex: 2}
	capture := &conversationCapture{pending: pending, info: info, metadata: make(map[string]string)}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(conversationCaptureContextKey, capture)
	req := httptest.NewRequest(http.MethodPost, "https://provider.example/upload?api-version=2026-01-01&secret=hidden", strings.NewReader("request-body"))
	req.Header.Add("OpenAI-Beta", "assistants=v2")
	operation := CaptureConversationAuxiliaryRequest(ctx, info, "file upload", req)
	requestBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NoError(t, req.Body.Close())
	assert.Equal(t, []byte("request-body"), requestBody)
	responseBody := `{"ok":true}`
	resp := &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody)), ContentLength: int64(len(responseBody))}
	CaptureConversationAuxiliaryResponse(ctx, operation, resp)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, pending.ExcludeFromRecord(capture.recoveryFiles[operation+"_response"]))

	result, err := pending.Finalize(conversationarchive.Record{
		ID: "aux-1", RecordedAt: recordedAt, Protocol: "openai", Metadata: capture.metadata,
		Completeness: conversationarchive.Completeness{Complete: true},
	})
	require.NoError(t, err)
	restored, _, err := store.Restore(result.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("request-body"), restored.Payloads[operation+"_request"])
	assert.Equal(t, []byte(responseBody), restored.Payloads[operation+"_response"])
	_, hasRecoveryCopy := restored.Payloads["recovery_raw_"+operation+"_response"]
	assert.False(t, hasRecoveryCopy)
	upstreamURL, err := url.Parse(restored.Metadata[operation+"_url"])
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01", upstreamURL.Query().Get("api-version"))
	assert.Equal(t, "***masked***", upstreamURL.Query().Get("secret"))
	assert.NotContains(t, restored.Metadata[operation+"_url"], "hidden")
	assert.Equal(t, `["assistants=v2"]`, restored.Metadata[operation+"_header_openai_beta"])
}

func TestCustomTransportEndpointKeepsSemanticQueryAndMasksCredentials(t *testing.T) {
	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 8, 15, 0, 0, time.UTC)
	pending, err := store.BeginPending(conversationarchive.Record{ID: "custom-transport", RecordedAt: recordedAt, Protocol: "openai"})
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
	capture := &conversationCapture{pending: pending, info: info, metadata: make(map[string]string)}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(conversationCaptureContextKey, capture)
	CaptureConversationUpstreamEndpoint(ctx, info, http.MethodGet, "wss://provider.example/chat?api-version=2026-01-01&authorization=secret")
	CaptureConversationUpstreamStatus(ctx, info, http.StatusSwitchingProtocols)

	storedURL, err := url.Parse(capture.metadata["upstream_attempt_000_url"])
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01", storedURL.Query().Get("api-version"))
	assert.Equal(t, "***masked***", storedURL.Query().Get("authorization"))
	assert.NotContains(t, capture.metadata["upstream_attempt_000_url"], "secret")
	assert.Equal(t, "101", capture.metadata["upstream_attempt_000_status"])
	require.NoError(t, pending.Preserve(capture.metadata, []string{"test_cleanup"}))
}

func TestMarkConversationCaptureIncompleteRecordsRelayPanic(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	capture := &conversationCapture{}
	ctx.Set(conversationCaptureContextKey, capture)
	MarkConversationCaptureIncomplete(ctx, "relay_panicked")
	assert.Equal(t, []string{"relay_panicked"}, capture.missing)
}

func TestInterruptedMediaFilterRetainsRawRecoveryPayload(t *testing.T) {
	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 8, 30, 0, 0, time.UTC)
	pending, err := store.BeginPending(conversationarchive.Record{ID: "media-crash", RecordedAt: recordedAt, Protocol: "openai_responses"})
	require.NoError(t, err)
	capture := &conversationCapture{
		pending: pending,
		info:    &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses},
	}
	partial := []byte(`data: {"type":"response.output_text.delta","delta":"尚未完整`)
	capture.appendPayload("client_response", partial)
	require.NoError(t, pending.Preserve(nil, []string{"process_interrupted"}))
	recovered, err := store.RecoverPending()
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	record, _, err := store.Restore(recovered[0].RecordPath)
	require.NoError(t, err)
	assert.Equal(t, partial, record.Payloads["recovery_raw_client_response"])
	assert.False(t, record.Completeness.Complete)
}

func TestCaptureConversationRealtimeFramePreservesOrderAndOmitsAudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	pending, err := store.BeginPending(conversationarchive.Record{ID: "realtime-1", RecordedAt: recordedAt, Protocol: "openai-realtime"})
	require.NoError(t, err)
	capture := &conversationCapture{pending: pending}
	ctx.Set(conversationCaptureContextKey, capture)

	audio := strings.Repeat("QUJD", 100)
	CaptureConversationRealtimeFrame(ctx, "client_to_upstream", []byte(`{"type":"input_audio_buffer.append","audio":"`+audio+`"}`))
	CaptureConversationRealtimeFrame(ctx, "upstream_to_client", []byte(`{"type":"response.audio_transcript.delta","delta":"真实转写文本"}`))

	result, err := pending.Finalize(conversationarchive.Record{
		ID:           "realtime-1",
		RecordedAt:   recordedAt,
		Protocol:     "openai-realtime",
		Completeness: conversationarchive.Completeness{Complete: true},
	})
	require.NoError(t, err)
	restored, _, err := store.Restore(result.RecordPath)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(restored.Payloads["realtime_events_ndjson"]), []byte{'\n'})
	require.Len(t, lines, 2)

	var first realtimeArchiveFrame
	require.NoError(t, common.Unmarshal(lines[0], &first))
	assert.Equal(t, uint64(1), first.Sequence)
	assert.Equal(t, "client_to_upstream", first.Direction)
	assert.NotContains(t, string(first.Frame), audio)
	require.Len(t, first.AudioOmissions, 1)
	assert.Equal(t, "$.audio", first.AudioOmissions[0].Path)
	assert.Len(t, first.AudioOmissions[0].SHA256, 64)

	var second realtimeArchiveFrame
	require.NoError(t, common.Unmarshal(lines[1], &second))
	assert.Equal(t, uint64(2), second.Sequence)
	assert.Contains(t, string(second.Frame), "真实转写文本")
	assert.Empty(t, second.AudioOmissions)
}

func TestConversationCaptureFinishWritesRestorableRecordBeforeSmallManifest(t *testing.T) {
	root := t.TempDir()
	store, err := conversationarchive.New(root, conversationarchive.Options{})
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	startedAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	request := []byte(`{"messages":[{"role":"user","content":"完整输入"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"完整输出"}}]}`)
	pending, err := store.BeginPending(conversationarchive.Record{
		ID:         "request-archive-1",
		RecordedAt: startedAt,
		Protocol:   string(types.RelayFormatOpenAI),
	})
	require.NoError(t, err)
	require.NoError(t, pending.Append("client_request", request))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"完整输入"}]}`))
	ctx.Set("username", "training-user")
	capture := &conversationCapture{
		pending:      pending,
		info:         &relaycommon.RelayInfo{RequestId: "request-archive-1", RelayFormat: types.RelayFormatOpenAI, StartTime: startedAt},
		metadata:     map[string]string{"request_path": "/v1/chat/completions"},
		startedAt:    startedAt,
		requestBytes: int64(len(request)),
	}
	capture.appendPayload("client_response", response)

	require.NoError(t, capture.Finish(ctx, nil))
	var manifest model.ConversationArchive
	require.NoError(t, db.First(&manifest).Error)
	assert.NotEmpty(t, manifest.RecordPath)
	assert.Len(t, manifest.RecordSha256, 64)
	assert.NotContains(t, manifest.RequestPath, "完整输入")

	restored, result, err := store.Restore(manifest.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, manifest.RecordSha256, result.SHA256)
	assert.Equal(t, request, restored.Payloads["client_request"])
	assert.Equal(t, response, restored.Payloads["client_response"])
	_, hasRecoveryCopy := restored.Payloads["recovery_raw_client_response"]
	assert.False(t, hasRecoveryCopy)
	assert.True(t, restored.Completeness.Complete)
}
