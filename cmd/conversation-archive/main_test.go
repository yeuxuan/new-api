package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/conversationarchive"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyRecordPreservesSourceBytesAndMarksKnownGaps(t *testing.T) {
	legacy := model.ConversationLog{
		Id:        42,
		RequestId: "request-42",
		CreatedAt: 1_700_000_000,
		Messages:  " {\"messages\": [\"原始空白\"]} ",
		Response:  "data: {\"text\":\"回答\"}\n\n",
	}

	record := legacyRecord(legacy, "legacy-42")
	assert.Equal(t, []byte(legacy.Messages), record.Payloads["legacy_messages"])
	assert.Equal(t, []byte(legacy.Response), record.Payloads["legacy_response"])
	assert.False(t, record.Completeness.Complete)
	assert.Contains(t, record.Completeness.Missing, "upstream_attempts")
}

func TestCheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration", "checkpoint.json")
	want := migrationCheckpoint{
		Version:        1,
		SourceTable:    "conversation_logs",
		HighWaterID:    100,
		LastArchivedID: 40,
		UpdatedAt:      time.Now().UTC(),
	}
	require.NoError(t, saveCheckpoint(path, want))
	got, err := loadCheckpoint(path)
	require.NoError(t, err)
	assert.Equal(t, want.Version, got.Version)
	assert.Equal(t, want.SourceTable, got.SourceTable)
	assert.Equal(t, want.HighWaterID, got.HighWaterID)
	assert.Equal(t, want.LastArchivedID, got.LastArchivedID)
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestValidateArchiveOperationalPathStaysInNamespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, validateArchiveOperationalPath(root, filepath.Join(root, "exports", "raw.jsonl.gz"), "exports", ".gz"))
	require.Error(t, validateArchiveOperationalPath(root, filepath.Join(root, "records", "raw.jsonl.gz"), "exports", ".gz"))
	require.Error(t, validateArchiveOperationalPath(root, filepath.Join(root, "exports", "raw.jsonl"), "exports", ".gz"))
	require.Error(t, validateArchiveOperationalPath(root, filepath.Join(root, "exports"), "exports", ".gz"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "exports"), 0o700))
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "exports", "linked")))
	require.Error(t, validateArchiveOperationalPath(root, filepath.Join(root, "exports", "linked", "raw.jsonl.gz"), "exports", ".gz"))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "target.jsonl.gz"), []byte("outside"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "target.jsonl.gz"), filepath.Join(root, "exports", "target.jsonl.gz")))
	require.Error(t, validateArchiveOperationalPath(root, filepath.Join(root, "exports", "target.jsonl.gz"), "exports", ".gz"))
}

func TestSameLegacyRecordDetectsSourceChanges(t *testing.T) {
	legacy := model.ConversationLog{Id: 7, CreatedAt: 1_700_000_000, Messages: "before", Response: "answer"}
	expected := legacyRecord(legacy, "legacy-7")
	actual := legacyRecord(legacy, "legacy-7")
	assert.True(t, sameLegacyRecord(actual, expected))
	actual.Payloads["legacy_messages"] = []byte("after")
	assert.False(t, sameLegacyRecord(actual, expected))
}

func TestReindexArchivesRebuildsMissingManifest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	_, err = store.Write(conversationarchive.Record{
		ID:         "orphan-1",
		RecordedAt: time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
		Protocol:   "openai",
		Payloads: map[string][]byte{
			"client_request":  []byte("request"),
			"client_response": []byte("response"),
		},
		Metadata: map[string]string{
			"request_id":       "req-orphan-1",
			"user_id":          "7",
			"client_status":    "200",
			"training_consent": "unknown",
		},
		Completeness: conversationarchive.Completeness{Complete: true},
	})
	require.NoError(t, err)
	require.NoError(t, reindexArchives(store))
	var manifest model.ConversationArchive
	require.NoError(t, db.Where("archive_id = ?", "orphan-1").First(&manifest).Error)
	assert.Equal(t, 7, manifest.UserId)
	assert.Equal(t, int64(len("request")), manifest.RequestBytes)
	assert.Equal(t, int64(len("response")), manifest.ResponseBytes)
	assert.True(t, manifest.Complete)
}

func TestVerifyArchivesDetectsDatabaseManifestDrift(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	record := conversationarchive.Record{
		ID: "manifest-drift", RecordedAt: time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC), Protocol: "openai",
		Payloads:     map[string][]byte{"client_request": []byte("request"), "client_response": []byte("response")},
		Metadata:     map[string]string{"request_id": "req-manifest-drift", "client_status": "200"},
		Completeness: conversationarchive.Completeness{Complete: true},
	}
	result, err := store.Write(record)
	require.NoError(t, err)
	manifest := manifestFromRecord(record, result)
	manifest.RequestBytes++
	require.NoError(t, db.Create(&manifest).Error)

	err = verifyArchives(options{batchSize: 5}, store)
	require.ErrorContains(t, err, "database manifest does not match immutable record")
}

func TestVerifyOnlineArchivesAllowsActivePending(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)
	record := conversationarchive.Record{
		ID: "online-verified", RecordedAt: time.Date(2026, 8, 2, 9, 45, 0, 0, time.UTC), Protocol: "openai",
		Payloads:     map[string][]byte{"client_request": []byte("request"), "client_response": []byte("response")},
		Metadata:     map[string]string{"request_id": "req-online-verified", "client_status": "200"},
		Completeness: conversationarchive.Completeness{Complete: true},
	}
	result, err := store.Write(record)
	require.NoError(t, err)
	manifest := manifestFromRecord(record, result)
	require.NoError(t, db.Create(&manifest).Error)
	pending, err := store.BeginPending(conversationarchive.Record{
		ID: "active-request", RecordedAt: time.Date(2026, 8, 2, 9, 46, 0, 0, time.UTC), Protocol: "openai",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pending.Preserve(nil, []string{"test_cleanup"}) })

	require.NoError(t, verifyOnlineArchives(options{batchSize: 5}, store))
	require.ErrorContains(t, verifyArchives(options{batchSize: 5}, store), "unfinished conversation capture")
}

func TestVerifyArchiveManifestsStopsAtHighWater(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	store, err := conversationarchive.New(filepath.Join(t.TempDir(), "archive"), conversationarchive.Options{})
	require.NoError(t, err)

	writeManifest := func(id string, recordedAt time.Time) model.ConversationArchive {
		record := conversationarchive.Record{
			ID: id, RecordedAt: recordedAt, Protocol: "openai",
			Payloads:     map[string][]byte{"client_request": []byte(id), "client_response": []byte("response")},
			Metadata:     map[string]string{"request_id": "req-" + id, "client_status": "200"},
			Completeness: conversationarchive.Completeness{Complete: true},
		}
		result, writeErr := store.Write(record)
		require.NoError(t, writeErr)
		manifest := manifestFromRecord(record, result)
		require.NoError(t, db.Create(&manifest).Error)
		return manifest
	}
	first := writeManifest("before-high-water", time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	writeManifest("after-high-water", time.Date(2026, 8, 2, 10, 1, 0, 0, time.UTC))

	verified, indexedPaths, err := verifyArchiveManifests(options{batchSize: 1}, store, first.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), verified)
	assert.Contains(t, indexedPaths, filepath.ToSlash(filepath.Clean(first.RecordPath)))
	assert.Len(t, indexedPaths, 1)
}

func TestManifestTreatsMissingPartsAsIncomplete(t *testing.T) {
	record := conversationarchive.Record{
		ID: "missing-tail", RecordedAt: time.Now(), Protocol: "openai",
		Completeness: conversationarchive.Completeness{Complete: true, Missing: []string{"stream_tail"}},
	}
	manifest := manifestFromRecord(record, conversationarchive.Result{RecordPath: "records/test.json.gz"})
	assert.False(t, manifest.Complete)
}

func TestTrainingExportTreatsMissingPartsAsIncomplete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ConversationArchive{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	root := filepath.Join(t.TempDir(), "archive")
	store, err := conversationarchive.New(root, conversationarchive.Options{})
	require.NoError(t, err)
	record := conversationarchive.Record{
		ID: "training-incomplete", RecordedAt: time.Now(), Protocol: "openai",
		Completeness: conversationarchive.Completeness{Complete: true, Missing: []string{"stream_tail"}},
	}
	result, err := store.Write(record)
	require.NoError(t, err)
	manifest := manifestFromRecord(record, result)
	require.NoError(t, db.Create(&manifest).Error)
	output := filepath.Join(root, "exports", "training.jsonl.gz")
	require.NoError(t, exportArchives(options{output: output, batchSize: 5}, store))

	file, err := os.Open(output)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	var row trainingExportRow
	require.NoError(t, common.Unmarshal(bytes.TrimSpace(data), &row))
	assert.False(t, row.Complete)
	assert.Equal(t, []string{"stream_tail"}, row.Missing)
}
