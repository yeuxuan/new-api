package conversationarchive

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
