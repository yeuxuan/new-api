package service

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/conversationarchive"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCaptureConversationRealtimeFramePreservesOrderAndOmitsAudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	capture := &conversationCapture{payloads: make(map[string]*bytes.Buffer)}
	ctx.Set(conversationCaptureContextKey, capture)

	audio := strings.Repeat("QUJD", 100)
	CaptureConversationRealtimeFrame(ctx, "client_to_upstream", []byte(`{"type":"input_audio_buffer.append","audio":"`+audio+`"}`))
	CaptureConversationRealtimeFrame(ctx, "upstream_to_client", []byte(`{"type":"response.audio_transcript.delta","delta":"真实转写文本"}`))

	lines := bytes.Split(bytes.TrimSpace(capture.payloads["realtime_events_ndjson"].Bytes()), []byte{'\n'})
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
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"完整输入"}]}`))
	ctx.Set("username", "training-user")
	capture := &conversationCapture{
		store:     store,
		info:      &relaycommon.RelayInfo{RequestId: "request-archive-1", RelayFormat: types.RelayFormatOpenAI, StartTime: startedAt},
		request:   []byte(`{"messages":[{"role":"user","content":"完整输入"}]}`),
		payloads:  map[string]*bytes.Buffer{"client_response": bytes.NewBufferString(`{"choices":[{"message":{"content":"完整输出"}}]}`)},
		metadata:  map[string]string{"request_path": "/v1/chat/completions"},
		startedAt: startedAt,
	}

	require.NoError(t, capture.Finish(ctx, nil))
	var manifest model.ConversationArchive
	require.NoError(t, db.First(&manifest).Error)
	assert.NotEmpty(t, manifest.RecordPath)
	assert.Len(t, manifest.RecordSha256, 64)
	assert.NotContains(t, manifest.RequestPath, "完整输入")

	restored, result, err := store.Restore(manifest.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, manifest.RecordSha256, result.SHA256)
	assert.Equal(t, capture.request, restored.Payloads["client_request"])
	assert.Equal(t, capture.payloads["client_response"].Bytes(), restored.Payloads["client_response"])
	assert.True(t, restored.Completeness.Complete)
}
