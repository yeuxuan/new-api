package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/conversationarchive"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const conversationCaptureContextKey = "conversation_archive_capture"

var (
	conversationStoreMu sync.RWMutex
	conversationStore   *conversationarchive.Store
)

// InitConversationArchive validates the configured mount guard and initializes
// the immutable filesystem archive. A configured archive is fail-closed: the
// gateway must not silently recreate the directory on the root filesystem when
// the intended data disk is missing.
func InitConversationArchive() error {
	if !common.ConversationLogEnabled || common.ConversationLogStoragePath == "" {
		return nil
	}
	root := common.ConversationLogStoragePath
	if sentinel := common.ConversationLogStorageSentinel; sentinel != "" {
		if filepath.Base(sentinel) != sentinel || sentinel == "." || sentinel == ".." {
			return fmt.Errorf("invalid conversation archive sentinel %q", sentinel)
		}
		info, err := os.Lstat(filepath.Join(root, sentinel))
		if err != nil {
			return fmt.Errorf("conversation archive mount sentinel is unavailable: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("conversation archive mount sentinel is not a regular file")
		}
	}
	store, err := conversationarchive.New(root, conversationarchive.Options{
		BlobThreshold: common.ConversationLogBlobThreshold,
		MinFreeBytes:  common.ConversationLogMinFreeBytes,
	})
	if err != nil {
		return err
	}
	conversationStoreMu.Lock()
	conversationStore = store
	conversationStoreMu.Unlock()
	common.SysLog("conversation archive initialized at " + root)
	return nil
}

func getConversationStore() *conversationarchive.Store {
	conversationStoreMu.RLock()
	defer conversationStoreMu.RUnlock()
	return conversationStore
}

type conversationCapture struct {
	mu           sync.Mutex
	store        *conversationarchive.Store
	info         *relaycommon.RelayInfo
	request      []byte
	payloads     map[string]*bytes.Buffer
	metadata     map[string]string
	missing      []string
	responseErr  error
	startedAt    time.Time
	nextSequence uint64
}

type conversationCaptureWriter struct {
	gin.ResponseWriter
	capture *conversationCapture
}

func (w *conversationCaptureWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		w.capture.appendPayload("client_response", data[:n])
	}
	if err != nil {
		w.capture.noteResponseError(err)
	}
	return n, err
}

func (w *conversationCaptureWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	if n > 0 {
		w.capture.appendPayload("client_response", []byte(data[:n]))
	}
	if err != nil {
		w.capture.noteResponseError(err)
	}
	return n, err
}

// BeginConversationCapture snapshots the original client request and installs
// a response tee. It is called only after the request has been parsed far
// enough to apply the protocol inclusion policy.
func BeginConversationCapture(c *gin.Context, info *relaycommon.RelayInfo) (*conversationCapture, error) {
	if !common.ConversationLogEnabled || common.ConversationLogStoragePath == "" || !ShouldArchiveConversation(info) {
		return nil, nil
	}
	store := getConversationStore()
	if store == nil {
		return nil, errors.New("conversation archive is enabled but not initialized")
	}
	if err := store.CheckWritable(0); err != nil {
		return nil, err
	}
	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, fmt.Errorf("read conversation request body: %w", err)
	}
	request, err := bodyStorage.Bytes()
	if err != nil {
		return nil, fmt.Errorf("snapshot conversation request body: %w", err)
	}
	capture := &conversationCapture{
		store:     store,
		info:      info,
		request:   bytes.Clone(request),
		payloads:  make(map[string]*bytes.Buffer),
		metadata:  make(map[string]string),
		startedAt: info.StartTime,
	}
	if capture.startedAt.IsZero() {
		capture.startedAt = time.Now()
	}
	capture.metadata["client_content_type"] = c.GetHeader("Content-Type")
	capture.metadata["client_content_encoding"] = c.GetHeader("Content-Encoding")
	capture.metadata["client_accept"] = c.GetHeader("Accept")
	capture.metadata["client_protocol"] = string(info.RelayFormat)
	capture.metadata["request_path"] = c.Request.URL.Path
	if beta := c.Query("beta"); beta != "" {
		capture.metadata["query_beta"] = beta
	}
	if alt := c.Query("alt"); alt != "" {
		capture.metadata["query_alt"] = alt
	}
	c.Writer = &conversationCaptureWriter{ResponseWriter: c.Writer, capture: capture}
	c.Set(conversationCaptureContextKey, capture)
	return capture, nil
}

func getConversationCapture(c *gin.Context) *conversationCapture {
	if c == nil {
		return nil
	}
	value, ok := c.Get(conversationCaptureContextKey)
	if !ok {
		return nil
	}
	capture, _ := value.(*conversationCapture)
	return capture
}

// FinishConversationCapture finalizes the capture attached to ctx, if any.
func FinishConversationCapture(ctx *gin.Context, relayErr *types.NewAPIError) error {
	capture := getConversationCapture(ctx)
	if capture == nil {
		return nil
	}
	return capture.Finish(ctx, relayErr)
}

func (c *conversationCapture) appendPayload(name string, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buffer := c.payloads[name]
	if buffer == nil {
		buffer = bytes.NewBuffer(nil)
		c.payloads[name] = buffer
	}
	_, _ = buffer.Write(data)
}

func (c *conversationCapture) setMetadata(name, value string) {
	if c == nil || name == "" || value == "" {
		return
	}
	c.mu.Lock()
	c.metadata[name] = value
	c.mu.Unlock()
}

func (c *conversationCapture) noteResponseError(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	if c.responseErr == nil {
		c.responseErr = err
		c.appendMissingLocked("client_response_write_failed")
	}
	c.mu.Unlock()
}

func (c *conversationCapture) appendMissing(reason string) {
	if c == nil || reason == "" {
		return
	}
	c.mu.Lock()
	c.appendMissingLocked(reason)
	c.mu.Unlock()
}

func (c *conversationCapture) appendMissingLocked(reason string) {
	for _, existing := range c.missing {
		if existing == reason {
			return
		}
	}
	c.missing = append(c.missing, reason)
}

type captureReadCloser struct {
	io.ReadCloser
	capture  *conversationCapture
	name     string
	expected int64
	read     int64
	complete bool
}

func (r *captureReadCloser) Read(data []byte) (int, error) {
	n, err := r.ReadCloser.Read(data)
	if n > 0 {
		r.capture.appendPayload(r.name, data[:n])
		r.read += int64(n)
	}
	if err == io.EOF || (r.expected >= 0 && r.read >= r.expected) {
		r.complete = true
	}
	return n, err
}

func (r *captureReadCloser) Close() error {
	if !r.complete {
		r.capture.appendMissing(r.name + "_not_fully_read")
	}
	err := r.ReadCloser.Close()
	if err != nil {
		r.capture.appendMissing(r.name + "_close_failed")
	}
	return err
}

// CaptureConversationUpstreamRequest wraps the actual body handed to the HTTP
// transport, preserving each retry attempt independently.
func CaptureConversationUpstreamRequest(c *gin.Context, info *relaycommon.RelayInfo, req *http.Request) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || req == nil {
		return
	}
	attempt := info.RetryIndex
	prefix := fmt.Sprintf("upstream_attempt_%03d", attempt)
	capture.setMetadata(prefix+"_method", req.Method)
	if req.URL != nil {
		capture.setMetadata(prefix+"_url", req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
	}
	capture.setMetadata(prefix+"_protocol", string(info.GetFinalRequestRelayFormat()))
	if chain, err := common.Marshal(info.RequestConversionChain); err == nil {
		capture.setMetadata(prefix+"_conversion_chain", string(chain))
	}
	if req.Body != nil {
		req.Body = &captureReadCloser{
			ReadCloser: req.Body,
			capture:    capture,
			name:       prefix + "_request",
			expected:   req.ContentLength,
			complete:   req.ContentLength == 0,
		}
	}
}

func CaptureConversationUpstreamResponse(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || resp == nil {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.setMetadata(prefix+"_status", strconv.Itoa(resp.StatusCode))
	capture.setMetadata(prefix+"_content_type", resp.Header.Get("Content-Type"))
	if resp.Body != nil {
		resp.Body = &captureReadCloser{
			ReadCloser: resp.Body,
			capture:    capture,
			name:       prefix + "_response",
			expected:   resp.ContentLength,
			complete:   resp.ContentLength == 0,
		}
	}
}

func CaptureConversationUpstreamError(c *gin.Context, info *relaycommon.RelayInfo, err error) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || err == nil {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.setMetadata(prefix+"_transport_error", common.LocalLogPreview(err.Error()))
}

// CaptureConversationUpstreamBytes records protocol paths that do not use the
// shared net/http transport (for example AWS SDK and private WebSocket APIs).
func CaptureConversationUpstreamBytes(c *gin.Context, info *relaycommon.RelayInfo, direction string, data []byte) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || len(data) == 0 {
		return
	}
	if direction != "request" && direction != "response" {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.appendPayload(prefix+"_"+direction, data)
}

type realtimeArchiveFrame struct {
	Sequence       uint64                  `json:"sequence"`
	Direction      string                  `json:"direction"`
	CapturedAt     time.Time               `json:"captured_at"`
	EventType      string                  `json:"event_type,omitempty"`
	Frame          json.RawMessage         `json:"frame"`
	FrameSHA256    string                  `json:"frame_sha256"`
	AudioOmissions []realtimeAudioOmission `json:"audio_omissions,omitempty"`
}

type realtimeAudioOmission struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	EncodedBytes int    `json:"encoded_bytes"`
	DecodedBytes int    `json:"decoded_bytes"`
}

// CaptureConversationRealtimeFrame preserves the ordered Realtime text, tool,
// transcript and control events. Audio payloads are intentionally excluded by
// policy; their paths, hashes and sizes remain so the omission is explicit and
// auditable instead of silently making a record look complete.
func CaptureConversationRealtimeFrame(ctx *gin.Context, direction string, frame []byte) {
	capture := getConversationCapture(ctx)
	if capture == nil || len(frame) == 0 || (direction != "client_to_upstream" && direction != "upstream_to_client") {
		return
	}

	var event map[string]any
	eventType := ""
	omissions := make([]realtimeAudioOmission, 0)
	archivedFrame := bytes.Clone(frame)
	if err := common.Unmarshal(frame, &event); err == nil {
		if value, ok := event["type"].(string); ok {
			eventType = value
		}
		redactRealtimeAudio(event, "$", eventType, &omissions)
		if len(omissions) > 0 {
			if redacted, err := common.Marshal(event); err == nil {
				archivedFrame = redacted
			} else {
				capture.mu.Lock()
				capture.missing = append(capture.missing, "realtime_audio_redaction_failed")
				capture.mu.Unlock()
				return
			}
		}
	}

	digest := sha256.Sum256(frame)
	capture.mu.Lock()
	capture.nextSequence++
	entry, err := common.Marshal(realtimeArchiveFrame{
		Sequence:       capture.nextSequence,
		Direction:      direction,
		CapturedAt:     time.Now().UTC(),
		EventType:      eventType,
		Frame:          json.RawMessage(archivedFrame),
		FrameSHA256:    hex.EncodeToString(digest[:]),
		AudioOmissions: omissions,
	})
	if err != nil {
		capture.missing = append(capture.missing, "realtime_frame_encoding_failed")
		capture.mu.Unlock()
		return
	}
	buffer := capture.payloads["realtime_events_ndjson"]
	if buffer == nil {
		buffer = bytes.NewBuffer(nil)
		capture.payloads["realtime_events_ndjson"] = buffer
	}
	_, _ = buffer.Write(entry)
	_ = buffer.WriteByte('\n')
	capture.mu.Unlock()
}

func redactRealtimeAudio(value any, path, eventType string, omissions *[]realtimeAudioOmission) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			childPath := path + "." + key
			omit := strings.EqualFold(key, "audio") || (strings.EqualFold(key, "delta") && eventType == dto.RealtimeEventResponseAudioDelta)
			if omit {
				encoded, ok := child.(string)
				if !ok || encoded == "" {
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					decoded = []byte(encoded)
				}
				digest := sha256.Sum256(decoded)
				*omissions = append(*omissions, realtimeAudioOmission{
					Path:         childPath,
					SHA256:       hex.EncodeToString(digest[:]),
					EncodedBytes: len(encoded),
					DecodedBytes: len(decoded),
				})
				delete(current, key)
				continue
			}
			redactRealtimeAudio(child, childPath, eventType, omissions)
		}
	case []any:
		for index, child := range current {
			redactRealtimeAudio(child, fmt.Sprintf("%s[%d]", path, index), eventType, omissions)
		}
	}
}

// Finish writes the immutable archive first and then inserts its small database
// manifest. A database-index failure never destroys the canonical file record.
func (c *conversationCapture) Finish(ctx *gin.Context, relayErr *types.NewAPIError) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	payloads := make(map[string][]byte, len(c.payloads)+1)
	payloads["client_request"] = bytes.Clone(c.request)
	for name, buffer := range c.payloads {
		payloads[name] = bytes.Clone(buffer.Bytes())
	}
	if _, ok := payloads["client_response"]; !ok {
		payloads["client_response"] = []byte{}
	}
	metadata := make(map[string]string, len(c.metadata)+12)
	for name, value := range c.metadata {
		metadata[name] = value
	}
	missing := append([]string(nil), c.missing...)
	c.mu.Unlock()

	completedAt := time.Now()
	info := c.info
	metadata["request_id"] = info.RequestId
	metadata["user_id"] = strconv.Itoa(info.UserId)
	metadata["username"] = ctx.GetString("username")
	metadata["token_id"] = strconv.Itoa(info.TokenId)
	metadata["token_name"] = ctx.GetString("token_name")
	channelID := info.GetChannelID()
	upstreamModel := info.GetUpstreamModelName()
	metadata["channel_id"] = strconv.Itoa(channelID)
	metadata["origin_model"] = info.OriginModelName
	metadata["upstream_model"] = upstreamModel
	metadata["upstream_protocol"] = string(info.GetFinalRequestRelayFormat())
	metadata["is_stream"] = strconv.FormatBool(info.IsStream)
	metadata["started_at"] = c.startedAt.UTC().Format(time.RFC3339Nano)
	metadata["completed_at"] = completedAt.UTC().Format(time.RFC3339Nano)
	metadata["client_status"] = strconv.Itoa(ctx.Writer.Status())
	metadata["training_consent"] = "unknown"
	if relayErr != nil {
		metadata["relay_error_code"] = string(relayErr.GetErrorCode())
		metadata["relay_error_type"] = string(relayErr.GetErrorType())
	}
	conversion, _ := common.Marshal(info.RequestConversionChain)
	metadata["request_conversion_chain"] = string(conversion)

	complete := len(missing) == 0
	recordID := strings.TrimPrefix(info.RequestId, "req_")
	if recordID == "" {
		recordID = common.NewRequestId()
	}
	result, err := c.store.Write(conversationarchive.Record{
		ID:         recordID,
		RecordedAt: c.startedAt,
		Protocol:   string(info.RelayFormat),
		Payloads:   payloads,
		Metadata:   metadata,
		Completeness: conversationarchive.Completeness{
			Complete: complete,
			Missing:  missing,
		},
	})
	if err != nil {
		return fmt.Errorf("persist conversation archive: %w", err)
	}

	responseBytes := int64(len(payloads["client_response"]))
	requestConversion := string(conversion)
	archive := &model.ConversationArchive{
		ArchiveId:            recordID,
		RequestId:            info.RequestId,
		UserId:               info.UserId,
		Username:             ctx.GetString("username"),
		TokenId:              info.TokenId,
		TokenName:            ctx.GetString("token_name"),
		ChannelId:            channelID,
		OriginModelName:      info.OriginModelName,
		UpstreamModelName:    upstreamModel,
		ClientProtocol:       string(info.RelayFormat),
		UpstreamProtocol:     string(info.GetFinalRequestRelayFormat()),
		RequestPath:          ctx.Request.URL.Path,
		CreatedAt:            c.startedAt.Unix(),
		CompletedAt:          completedAt.Unix(),
		StatusCode:           ctx.Writer.Status(),
		IsStream:             info.IsStream,
		Complete:             complete,
		TrainingConsent:      "unknown",
		RecordPath:           result.RecordPath,
		RecordSha256:         result.SHA256,
		RequestBytes:         int64(len(c.request)),
		ResponseBytes:        responseBytes,
		StoredBytes:          result.StoredBytes,
		RequestConversion:    requestConversion,
		ArchiveFormatVersion: conversationarchive.CurrentVersion,
	}
	if relayErr != nil {
		archive.ErrorCode = string(relayErr.GetErrorCode())
	}
	if err := model.InsertConversationArchive(archive); err != nil {
		logger.LogError(ctx, "conversation archive index insert failed: "+err.Error())
		return fmt.Errorf("insert conversation archive index: %w", err)
	}
	return nil
}
