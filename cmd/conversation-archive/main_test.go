package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
