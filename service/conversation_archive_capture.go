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
	store, err := conversationarchive.New(root, conversationarchive.Options{
		BlobThreshold: common.ConversationLogBlobThreshold,
		MinFreeBytes:  common.ConversationLogMinFreeBytes,
		MountSentinel: common.ConversationLogStorageSentinel,
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
	mu            sync.Mutex
	pending       *conversationarchive.PendingCapture
	info          *relaycommon.RelayInfo
	metadata      map[string]string
	missing       []string
	responseErr   error
	appendErr     error
	startedAt     time.Time
	requestBytes  int64
	nextSequence  uint64
	nextOperation uint64
	finishing     bool
	mediaFilters  map[string]*generatedMediaFilter
	mediaOmitted  int
	recoveryFiles map[string]string
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
	startedAt := info.StartTime
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	metadata := map[string]string{
		"client_content_type":     c.GetHeader("Content-Type"),
		"client_content_encoding": c.GetHeader("Content-Encoding"),
		"client_accept":           c.GetHeader("Accept"),
		"client_protocol":         string(info.RelayFormat),
		"request_path":            c.Request.URL.Path,
		"client_url":              relaycommon.SanitizeURLForLog(c.Request.URL.String()),
		"request_id":              info.RequestId,
		"user_id":                 strconv.Itoa(info.UserId),
		"username":                c.GetString("username"),
		"token_id":                strconv.Itoa(info.TokenId),
		"token_name":              c.GetString("token_name"),
		"origin_model":            info.OriginModelName,
		"is_stream":               strconv.FormatBool(info.IsStream),
		"started_at":              startedAt.UTC().Format(time.RFC3339Nano),
		"training_consent":        "unknown",
	}
	for name, value := range conversationProtocolHeaderMetadata(c.Request.Header) {
		metadata["client_"+name] = value
	}
	if conversion, marshalErr := common.Marshal(info.RequestConversionChain); marshalErr == nil {
		metadata["request_conversion_chain"] = string(conversion)
	}
	if beta := c.Query("beta"); beta != "" {
		metadata["query_beta"] = beta
	}
	if alt := c.Query("alt"); alt != "" {
		metadata["query_alt"] = alt
	}
	recordID := conversationArchiveRecordID(info.RequestId)
	pending, err := store.BeginPending(conversationarchive.Record{
		ID:         recordID,
		RecordedAt: startedAt,
		Protocol:   string(info.RelayFormat),
		Metadata:   metadata,
	})
	if err != nil {
		return nil, err
	}
	capture := &conversationCapture{
		pending:       pending,
		info:          info,
		metadata:      metadata,
		startedAt:     startedAt,
		requestBytes:  int64(len(request)),
		mediaFilters:  make(map[string]*generatedMediaFilter),
		recoveryFiles: make(map[string]string),
	}
	if hasUnpinnedConversationAttachment(request) {
		capture.metadata["external_attachment_state"] = "unpinned"
		capture.missing = append(capture.missing, "external_attachment_not_pinned")
	}
	if err := pending.Append("client_request", request); err != nil {
		_ = pending.Preserve(metadata, []string{"client_request_spool_failed"})
		return nil, fmt.Errorf("spool conversation request: %w", err)
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

func MarkConversationCaptureIncomplete(ctx *gin.Context, reason string) {
	capture := getConversationCapture(ctx)
	if capture != nil {
		capture.appendMissing(reason)
	}
}

func (c *conversationCapture) appendPayload(name string, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finishing {
		return
	}
	if c.appendErr != nil {
		return
	}
	if c.pending == nil {
		c.appendErr = errors.New("conversation archive pending capture is unavailable")
		c.appendMissingLocked("pending_capture_unavailable")
		return
	}
	archivedData := data
	if c.shouldFilterGeneratedMedia(name) {
		recoveryName := "recovery_raw_" + name
		if c.recoveryFiles == nil {
			c.recoveryFiles = make(map[string]string)
		}
		c.recoveryFiles[name] = recoveryName
		if err := c.pending.Append(recoveryName, data); err != nil {
			c.appendErr = err
			c.appendMissingLocked("recovery_payload_spool_failed")
			return
		}
		if c.mediaFilters == nil {
			c.mediaFilters = make(map[string]*generatedMediaFilter)
		}
		filter := c.mediaFilters[name]
		if filter == nil {
			filter = &generatedMediaFilter{}
			c.mediaFilters[name] = filter
		}
		var omissions int
		archivedData, omissions = filter.Append(data, false)
		c.mediaOmitted += omissions
	}
	if err := c.pending.Append(name, archivedData); err != nil {
		c.appendErr = err
		c.appendMissingLocked("payload_spool_failed")
	}
}

func (c *conversationCapture) shouldFilterGeneratedMedia(name string) bool {
	if c == nil || c.info == nil || (name != "client_response" && !strings.HasSuffix(name, "_response")) {
		return false
	}
	return true
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
	mu sync.Mutex
	io.ReadCloser
	capture  *conversationCapture
	name     string
	expected int64
	read     int64
	complete bool
}

func (r *captureReadCloser) Read(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	capture.setMetadata(prefix+"_content_type", req.Header.Get("Content-Type"))
	capture.setMetadata(prefix+"_content_encoding", req.Header.Get("Content-Encoding"))
	for name, value := range conversationProtocolHeaderMetadata(req.Header) {
		capture.setMetadata(prefix+"_"+name, value)
	}
	if req.URL != nil {
		capture.setMetadata(prefix+"_url", relaycommon.SanitizeURLForLog(req.URL.String()))
	}
	capture.setMetadata(prefix+"_protocol", string(info.GetFinalRequestRelayFormat()))
	capture.setMetadata(prefix+"_channel_id", strconv.Itoa(info.GetChannelID()))
	capture.setMetadata(prefix+"_channel_type", strconv.Itoa(info.GetChannelType()))
	capture.setMetadata(prefix+"_upstream_model", info.GetUpstreamModelName())
	capture.setMetadata(prefix+"_captured_at", time.Now().UTC().Format(time.RFC3339Nano))
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

// CaptureConversationAuxiliaryRequest records provider-specific HTTP calls
// such as polling, detail retrieval, and file upload that bypass DoApiRequest.
// The returned operation key must be passed to the response/error counterpart.
func CaptureConversationAuxiliaryRequest(c *gin.Context, info *relaycommon.RelayInfo, stage string, req *http.Request) string {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || req == nil {
		return ""
	}
	stage = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, stage)
	if stage == "" {
		stage = "auxiliary"
	}
	capture.mu.Lock()
	capture.nextOperation++
	sequence := capture.nextOperation
	capture.mu.Unlock()
	prefix := fmt.Sprintf("upstream_attempt_%03d_%s_%03d", info.RetryIndex, stage, sequence)
	capture.setMetadata(prefix+"_method", req.Method)
	capture.setMetadata(prefix+"_content_type", req.Header.Get("Content-Type"))
	capture.setMetadata(prefix+"_content_encoding", req.Header.Get("Content-Encoding"))
	for name, value := range conversationProtocolHeaderMetadata(req.Header) {
		capture.setMetadata(prefix+"_"+name, value)
	}
	if req.URL != nil {
		capture.setMetadata(prefix+"_url", relaycommon.SanitizeURLForLog(req.URL.String()))
	}
	capture.setMetadata(prefix+"_channel_id", strconv.Itoa(info.GetChannelID()))
	capture.setMetadata(prefix+"_channel_type", strconv.Itoa(info.GetChannelType()))
	capture.setMetadata(prefix+"_upstream_model", info.GetUpstreamModelName())
	capture.setMetadata(prefix+"_protocol", string(info.GetFinalRequestRelayFormat()))
	capture.setMetadata(prefix+"_captured_at", time.Now().UTC().Format(time.RFC3339Nano))
	if req.Body != nil {
		req.Body = &captureReadCloser{
			ReadCloser: req.Body,
			capture:    capture,
			name:       prefix + "_request",
			expected:   req.ContentLength,
			complete:   req.ContentLength == 0,
		}
	}
	return prefix
}

func CaptureConversationAuxiliaryResponse(c *gin.Context, operation string, resp *http.Response) {
	capture := getConversationCapture(c)
	if capture == nil || operation == "" || resp == nil {
		return
	}
	capture.setMetadata(operation+"_status", strconv.Itoa(resp.StatusCode))
	capture.setMetadata(operation+"_content_type", resp.Header.Get("Content-Type"))
	capture.setMetadata(operation+"_content_encoding", resp.Header.Get("Content-Encoding"))
	if resp.Body != nil {
		resp.Body = &captureReadCloser{
			ReadCloser: resp.Body,
			capture:    capture,
			name:       operation + "_response",
			expected:   resp.ContentLength,
			complete:   resp.ContentLength == 0,
		}
	}
}

func CaptureConversationAuxiliaryError(c *gin.Context, operation string, err error) {
	capture := getConversationCapture(c)
	if capture == nil || operation == "" || err == nil {
		return
	}
	capture.setMetadata(operation+"_transport_error", common.LocalLogPreview(err.Error()))
}

func conversationProtocolHeaderMetadata(header http.Header) map[string]string {
	metadata := make(map[string]string)
	for headerName, metadataName := range map[string]string{
		"Anthropic-Version": "header_anthropic_version",
		"Anthropic-Beta":    "header_anthropic_beta",
		"OpenAI-Beta":       "header_openai_beta",
	} {
		values := header.Values(headerName)
		if len(values) == 0 {
			continue
		}
		encoded, err := common.Marshal(values)
		if err == nil {
			metadata[metadataName] = string(encoded)
		}
	}
	return metadata
}

func CaptureConversationUpstreamResponse(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || resp == nil {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.setMetadata(prefix+"_status", strconv.Itoa(resp.StatusCode))
	capture.setMetadata(prefix+"_content_type", resp.Header.Get("Content-Type"))
	capture.setMetadata(prefix+"_content_encoding", resp.Header.Get("Content-Encoding"))
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

// CaptureConversationUpstreamEndpoint records request semantics for custom
// transports, such as provider WebSockets, that bypass http.Request wrappers.
func CaptureConversationUpstreamEndpoint(c *gin.Context, info *relaycommon.RelayInfo, method, rawURL string) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.setMetadata(prefix+"_method", method)
	capture.setMetadata(prefix+"_url", relaycommon.SanitizeURLForLog(rawURL))
	capture.setMetadata(prefix+"_protocol", string(info.GetFinalRequestRelayFormat()))
	capture.setMetadata(prefix+"_channel_id", strconv.Itoa(info.GetChannelID()))
	capture.setMetadata(prefix+"_channel_type", strconv.Itoa(info.GetChannelType()))
	capture.setMetadata(prefix+"_upstream_model", info.GetUpstreamModelName())
	capture.setMetadata(prefix+"_captured_at", time.Now().UTC().Format(time.RFC3339Nano))
}

func CaptureConversationUpstreamStatus(c *gin.Context, info *relaycommon.RelayInfo, statusCode int) {
	capture := getConversationCapture(c)
	if capture == nil || info == nil || statusCode == 0 {
		return
	}
	prefix := fmt.Sprintf("upstream_attempt_%03d", info.RetryIndex)
	capture.setMetadata(prefix+"_status", strconv.Itoa(statusCode))
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
	capture.setMetadata(prefix+"_protocol", string(info.GetFinalRequestRelayFormat()))
	capture.setMetadata(prefix+"_channel_id", strconv.Itoa(info.GetChannelID()))
	capture.setMetadata(prefix+"_channel_type", strconv.Itoa(info.GetChannelType()))
	capture.setMetadata(prefix+"_upstream_model", info.GetUpstreamModelName())
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
	if capture.finishing || capture.appendErr != nil {
		capture.mu.Unlock()
		return
	}
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
	entry = append(entry, '\n')
	if capture.pending == nil {
		capture.appendErr = errors.New("conversation archive pending capture is unavailable")
		capture.appendMissingLocked("pending_capture_unavailable")
	} else if err := capture.pending.Append("realtime_events_ndjson", entry); err != nil {
		capture.appendErr = err
		capture.appendMissingLocked("payload_spool_failed")
	}
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
	for name, filter := range c.mediaFilters {
		archivedData, omissions := filter.Append(nil, true)
		c.mediaOmitted += omissions
		if c.pending == nil {
			c.appendErr = errors.New("conversation archive pending capture is unavailable")
			c.appendMissingLocked("pending_capture_unavailable")
			break
		}
		if err := c.pending.Append(name, archivedData); err != nil && c.appendErr == nil {
			c.appendErr = err
			c.appendMissingLocked("payload_spool_failed")
		}
	}
	if c.appendErr == nil && c.pending != nil {
		recoveryNames := make([]string, 0, len(c.recoveryFiles))
		for _, name := range c.recoveryFiles {
			recoveryNames = append(recoveryNames, name)
		}
		if err := c.pending.ExcludeFromRecord(recoveryNames...); err != nil {
			c.appendErr = err
			c.appendMissingLocked("recovery_payload_exclusion_failed")
		}
	}
	c.finishing = true
	metadata := make(map[string]string, len(c.metadata)+12)
	for name, value := range c.metadata {
		metadata[name] = value
	}
	missing := append([]string(nil), c.missing...)
	appendErr := c.appendErr
	mediaOmitted := c.mediaOmitted
	c.mu.Unlock()
	if appendErr != nil {
		if c.pending != nil {
			_ = c.pending.Preserve(metadata, missing)
		}
		return fmt.Errorf("spool conversation archive: %w", appendErr)
	}
	if c.pending == nil {
		return errors.New("conversation archive pending capture is unavailable")
	}
	if c.pending.Size("client_response") == 0 {
		if err := c.pending.Append("client_response", []byte{}); err != nil {
			return fmt.Errorf("spool empty conversation response: %w", err)
		}
	}

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
	if mediaOmitted > 0 {
		metadata["generated_media_omissions"] = generatedMediaMetadata(mediaOmitted)
	}
	if relayErr != nil {
		metadata["relay_error_code"] = string(relayErr.GetErrorCode())
		metadata["relay_error_type"] = string(relayErr.GetErrorType())
	}
	conversion, _ := common.Marshal(info.RequestConversionChain)
	metadata["request_conversion_chain"] = string(conversion)

	complete := len(missing) == 0
	recordID := conversationArchiveRecordID(info.RequestId)
	result, err := c.pending.Finalize(conversationarchive.Record{
		ID:         recordID,
		RecordedAt: c.startedAt,
		Protocol:   string(info.RelayFormat),
		Metadata:   metadata,
		Completeness: conversationarchive.Completeness{
			Complete: complete,
			Missing:  missing,
		},
	})
	if err != nil {
		return fmt.Errorf("persist conversation archive: %w", err)
	}

	responseBytes := c.pending.Size("client_response")
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
		RequestBytes:         c.requestBytes,
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

func conversationArchiveRecordID(requestID string) string {
	source := requestID
	if source == "" {
		source = common.NewRequestId()
	}
	candidate := strings.TrimPrefix(source, "req_")
	if len(candidate) >= 1 && len(candidate) <= 64 {
		valid := true
		for index, char := range []byte(candidate) {
			if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') ||
				(index > 0 && (char == '.' || char == '_' || char == '-')) {
				continue
			}
			valid = false
			break
		}
		if valid {
			return candidate
		}
	}
	digest := sha256.Sum256([]byte(source))
	return "request-" + hex.EncodeToString(digest[:])[:56]
}
