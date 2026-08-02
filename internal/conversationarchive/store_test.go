package conversationarchive

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingCaptureSpoolsAndFinalizesLosslessly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	store, err := New(root, Options{BlobThreshold: 1024})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	pending, err := store.BeginPending(Record{ID: "spool-001", RecordedAt: recordedAt, Protocol: "openai_responses"})
	require.NoError(t, err)
	payload := []byte(strings.Repeat("真实流式响应", 2000))
	for offset := 0; offset < len(payload); offset += 257 {
		end := min(offset+257, len(payload))
		require.NoError(t, pending.Append("client_response", payload[offset:end]))
	}
	paths, err := store.PendingPaths()
	require.NoError(t, err)
	require.Len(t, paths, 1)

	result, err := pending.Finalize(Record{
		ID:           "spool-001",
		RecordedAt:   recordedAt,
		Protocol:     "openai_responses",
		Completeness: Completeness{Complete: true},
	})
	require.NoError(t, err)
	paths, err = store.PendingPaths()
	require.NoError(t, err)
	assert.Empty(t, paths)
	transient, err := store.TransientPaths()
	require.NoError(t, err)
	assert.Empty(t, transient)
	restored, _, err := store.Restore(result.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, payload, restored.Payloads["client_response"])
}

func TestPendingCapturePreservesIncompleteFiles(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	pending, err := store.BeginPending(Record{ID: "spool-failed", RecordedAt: time.Now(), Protocol: "openai"})
	require.NoError(t, err)
	require.NoError(t, pending.Append("client_request", []byte("important raw request")))
	require.NoError(t, pending.Preserve(map[string]string{"reason": "test"}, []string{"relay_interrupted"}))
	paths, err := store.PendingPaths()
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	recovered, err := store.RecoverPending()
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	record, _, err := store.Restore(recovered[0].RecordPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("important raw request"), record.Payloads["client_request"])
	assert.False(t, record.Completeness.Complete)
	assert.Contains(t, record.Completeness.Missing, "recovered_after_interruption")
	paths, err = store.PendingPaths()
	require.NoError(t, err)
	assert.Empty(t, paths)
	transient, err := store.TransientPaths()
	require.NoError(t, err)
	assert.Empty(t, transient)
}

func TestRecoverPendingRefusesActiveCapture(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 11, 30, 0, 0, time.UTC)
	pending, err := store.BeginPending(Record{ID: "still-active", RecordedAt: recordedAt, Protocol: "openai"})
	require.NoError(t, err)
	require.NoError(t, pending.Append("client_request", []byte("first")))

	_, err = store.RecoverPending()
	require.ErrorContains(t, err, "still active")
	require.NoError(t, pending.Append("client_response", []byte("second")))
	_, err = pending.Finalize(Record{
		ID: "still-active", RecordedAt: recordedAt, Protocol: "openai", Completeness: Completeness{Complete: true},
	})
	require.NoError(t, err)
}

func TestCapturingDescriptorDoesNotPersistRecoveryExclusionsEarly(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	pending, err := store.BeginPending(Record{ID: "exclude-window", RecordedAt: time.Now(), Protocol: "openai"})
	require.NoError(t, err)
	require.NoError(t, pending.Append("recovery_raw_client_response", []byte("raw")))
	require.NoError(t, pending.ExcludeFromRecord("recovery_raw_client_response"))
	require.NoError(t, pending.Append("client_response", nil), "creating another payload rewrites the capturing descriptor")

	descriptorData, err := os.ReadFile(filepath.Join(pending.dir, "pending.json"))
	require.NoError(t, err)
	var descriptor pendingDescriptor
	require.NoError(t, common.Unmarshal(descriptorData, &descriptor))
	assert.Empty(t, descriptor.Excluded)
	require.NoError(t, pending.Preserve(nil, []string{"test_cleanup"}))
}

func TestRecoverPendingReusesRecordWrittenBeforeCrash(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	recordedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	record := Record{ID: "crash-window", RecordedAt: recordedAt, Protocol: "openai", Metadata: map[string]string{"request_id": "crash-window"}, Completeness: Completeness{Complete: true}}
	pending, err := store.BeginPending(record)
	require.NoError(t, err)
	require.NoError(t, pending.Append("client_request", []byte("survives")))

	pending.mu.Lock()
	for _, file := range pending.files {
		require.NoError(t, file.Sync())
		require.NoError(t, file.Close())
	}
	pending.closed = true
	require.NoError(t, pending.writeDescriptorLocked("ready", record.Metadata, record.Completeness))
	payloadFiles := make(map[string]string, len(pending.paths))
	for name, path := range pending.paths {
		payloadFiles[name] = path
	}
	pending.mu.Unlock()
	_, err = store.writeFileRecord(record, payloadFiles)
	require.NoError(t, err)
	require.NoError(t, pending.releaseLock(), "simulate process exit after the immutable record became durable")

	recovered, err := store.RecoverPending()
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	records, err := store.RecordPaths()
	require.NoError(t, err)
	assert.Len(t, records, 1, "recovery must not duplicate an already durable record")
}

func TestOrphanBlobAndTransientFileDiscoveryIsReadOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	store, err := New(root, Options{BlobThreshold: 1})
	require.NoError(t, err)
	_, err = store.Write(Record{
		ID: "referenced-blob", RecordedAt: time.Now(), Protocol: "openai",
		Payloads: map[string][]byte{"request": []byte("referenced")}, Completeness: Completeness{Complete: true},
	})
	require.NoError(t, err)
	orphanDigest := strings.Repeat("a", 64)
	orphanPath := filepath.Join(root, "blobs", "sha256", "aa", orphanDigest+".gz")
	require.NoError(t, os.MkdirAll(filepath.Dir(orphanPath), 0o700))
	require.NoError(t, os.WriteFile(orphanPath, []byte("orphan"), 0o600))
	tempPath := filepath.Join(root, "blobs", "sha256", "aa", ".conversation-blob-crash.tmp")
	require.NoError(t, os.WriteFile(tempPath, []byte("partial"), 0o600))

	orphans, err := store.OrphanBlobPaths()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.ToSlash(filepath.Join("blobs", "sha256", "aa", orphanDigest+".gz"))}, orphans)
	transient, err := store.TransientPaths()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.ToSlash(filepath.Join("blobs", "sha256", "aa", ".conversation-blob-crash.tmp"))}, transient)
	_, err = os.Stat(orphanPath)
	assert.NoError(t, err, "discovery must never delete suspected leftovers")
}

func TestMountSentinelIsCheckedForEveryWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, os.MkdirAll(root, 0o700))
	sentinel := filepath.Join(root, ".archive-volume")
	require.NoError(t, os.WriteFile(sentinel, []byte("mounted"), 0o600))
	store, err := New(root, Options{MountSentinel: filepath.Base(sentinel)})
	require.NoError(t, err)
	require.NoError(t, os.Remove(sentinel))
	require.ErrorContains(t, store.CheckWritable(0), "mount sentinel is unavailable")
}

func TestMaintenanceLockSerializesArchiveCommands(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	first, err := store.AcquireMaintenanceLock()
	require.NoError(t, err)
	_, err = store.AcquireMaintenanceLock()
	require.ErrorContains(t, err, "already running")
	require.NoError(t, first.Close())
	second, err := store.AcquireMaintenanceLock()
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

func TestMountSentinelNeverCreatesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing", "archive")
	_, err := New(root, Options{MountSentinel: ".archive-volume"})
	require.ErrorContains(t, err, "must already exist")
	_, statErr := os.Lstat(root)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestNewRejectsBroadRootAndFinalSymlink(t *testing.T) {
	_, err := New(string(filepath.Separator), Options{})
	require.ErrorContains(t, err, "too broad")

	temp := t.TempDir()
	target := filepath.Join(temp, "real", "archive")
	require.NoError(t, os.MkdirAll(target, 0o700))
	link := filepath.Join(temp, "linked-archive")
	require.NoError(t, os.Symlink(target, link))
	_, err = New(link, Options{})
	require.ErrorContains(t, err, "must not be a symbolic link")
}

func TestWriteDeterministicIsIdempotentAndRejectsConflict(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)
	record := Record{
		ID:           "legacy-42",
		RecordedAt:   time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC),
		Protocol:     "legacy_conversation_log",
		Payloads:     map[string][]byte{"legacy_messages": []byte("hello")},
		Completeness: Completeness{Complete: false, Missing: []string{"upstream_attempts"}},
	}
	first, err := store.WriteDeterministic(record)
	require.NoError(t, err)
	second, err := store.WriteDeterministic(record)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	record.Payloads["legacy_messages"] = []byte("different")
	_, err = store.WriteDeterministic(record)
	require.ErrorContains(t, err, "different content")
}

func TestWriteRestoreDeduplicatesLargeStringsLosslessly(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "archive"), Options{})
	require.NoError(t, err)

	longText := strings.Repeat("真实用户对话 training text ", 4000)
	request := []byte(`{ "messages": [{"role":"user","content":` + quoteJSON(longText) + `}], "duplicate":` + quoteJSON(longText) + ` }`)
	record := Record{
		ID:           "chat-001",
		RecordedAt:   time.Date(2026, 8, 2, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60)),
		Protocol:     "openai-chat-completions",
		Payloads:     map[string][]byte{"empty": {}, "request": request, "response": request},
		Metadata:     map[string]string{"model": "gpt-test"},
		Completeness: Completeness{Complete: true},
	}

	result, err := store.Write(record)
	require.NoError(t, err)
	assert.Equal(t, IntegrityComplete, result.Integrity)
	assert.Contains(t, result.RecordPath, "records/2026/08/02/")
	assert.Len(t, result.SHA256, 64)
	assert.Greater(t, result.OriginalBytes, int64(0))
	assert.Greater(t, result.StoredBytes, int64(0))

	blobs, err := filepath.Glob(filepath.Join(root, "archive", "blobs", "sha256", "*", "*.gz"))
	require.NoError(t, err)
	assert.Len(t, blobs, 1, "identical large strings must share one content-addressed blob")

	restored, restoredResult, err := store.Restore(result.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, request, restored.Payloads["request"])
	assert.Equal(t, request, restored.Payloads["response"])
	assert.Empty(t, restored.Payloads["empty"])
	assert.Equal(t, record.Metadata, restored.Metadata)
	assert.Equal(t, result, restoredResult)

	record.ID = "chat-002"
	_, err = store.Write(record)
	require.NoError(t, err)
	blobs, err = filepath.Glob(filepath.Join(root, "archive", "blobs", "sha256", "*", "*.gz"))
	require.NoError(t, err)
	assert.Len(t, blobs, 1, "deduplication must work across records")

	assertMode(t, filepath.Join(root, "archive"), 0o700)
	assertMode(t, filepath.Join(root, "archive", filepath.FromSlash(result.RecordPath)), 0o600)
	assertMode(t, blobs[0], 0o600)
}

func TestWriteRestoreDataURIAndOpaqueBase64(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)

	rawImage := []byte(strings.Repeat("0123456789abcdef", 7000))
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(rawImage)
	jsonPayload := []byte(`{"role":"user","image_url":{"url":` + quoteJSON(dataURI) + `}}`)
	opaque := []byte(base64.StdEncoding.EncodeToString(rawImage))
	record := Record{
		ID:         "multimodal-001",
		RecordedAt: time.Date(2026, 8, 2, 23, 0, 0, 0, time.UTC),
		Protocol:   "anthropic-messages",
		Payloads: map[string][]byte{
			"request":     jsonPayload,
			"stream-body": opaque,
		},
		Completeness: Completeness{Complete: false, Missing: []string{"remote_attachment"}},
	}

	result, err := store.Write(record)
	require.NoError(t, err)
	assert.Equal(t, IntegrityIncomplete, result.Integrity)
	restored, _, err := store.Restore(result.RecordPath)
	require.NoError(t, err)
	assert.Equal(t, jsonPayload, restored.Payloads["request"])
	assert.Equal(t, opaque, restored.Payloads["stream-body"])
	assert.Equal(t, record.Completeness, restored.Completeness)
}

func TestWriteUsesConfiguredPartitionLocation(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{
		PartitionLocation: time.FixedZone("CST", 8*60*60),
	})
	require.NoError(t, err)

	result, err := store.Write(Record{
		ID:           "timezone-001",
		RecordedAt:   time.Date(2026, 8, 1, 17, 0, 0, 0, time.UTC),
		Protocol:     "openai-responses",
		Completeness: Completeness{Complete: true},
	})
	require.NoError(t, err)
	assert.Contains(t, result.RecordPath, "records/2026/08/02/")
}

func TestWriteAtomicRenameFailureLeavesNoPartialRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	store, err := New(root, Options{})
	require.NoError(t, err)
	store.rename = func(_, _ string) error { return errors.New("injected rename failure") }

	_, err = store.Write(Record{
		ID:           "atomic-001",
		RecordedAt:   time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC),
		Protocol:     "gemini-generate-content",
		Payloads:     map[string][]byte{"request": []byte(`{"message":"short"}`)},
		Completeness: Completeness{Complete: true},
	})
	require.ErrorContains(t, err, "injected rename failure")

	recordDir := filepath.Join(root, "records", "2026", "08", "02")
	entries, err := os.ReadDir(recordDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "temporary record must be removed after atomic rename failure")
}

func TestRejectsPathTraversal(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{})
	require.NoError(t, err)

	_, err = store.Write(Record{
		ID:         "../escape",
		RecordedAt: time.Now(),
		Protocol:   "openai-responses",
	})
	require.ErrorContains(t, err, "invalid conversation archive record id")

	for _, path := range []string{"../outside.json.gz", "/tmp/outside.json.gz", `records\\..\\outside.json.gz`, "blobs/sha256/aa/file.json.gz"} {
		_, _, err := store.Restore(path)
		assert.Error(t, err, path)
	}
}

func TestCheckWritableEnforcesSafetyReserve(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "archive"), Options{MinFreeBytes: int64(^uint64(0) >> 1)})
	require.NoError(t, err)
	require.ErrorContains(t, store.CheckWritable(0), "free space below reserve")
}

func quoteJSON(value string) string {
	// The test values contain no control characters or quotes. Keeping the
	// construction local avoids testing the project's JSON wrapper itself.
	return `"` + value + `"`
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm())
}
