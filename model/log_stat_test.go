package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSumUsedQuota(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	originalDB, originalGroupCol := LOG_DB, logGroupCol
	originalDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB, logGroupCol = originalDB, originalGroupCol
		common.SetLogDatabaseType(originalDatabaseType)
		require.NoError(t, sqlDB.Close())
	})
	LOG_DB, logGroupCol = db, "`group`"
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&Log{}))

	now := time.Now().Unix()
	logs := []Log{
		{CreatedAt: 100, Type: LogTypeConsume, Username: "alice", TokenName: "key-a", ModelName: "model-a", ChannelId: 1, Group: "group-a", Quota: 10000, PromptTokens: 100, CompletionTokens: 10},
		{CreatedAt: 200, Type: LogTypeConsume, Username: "alice", TokenName: "key-a", ModelName: "model-a", ChannelId: 1, Group: "group-a", Quota: 20000, PromptTokens: 200, CompletionTokens: 20},
		{CreatedAt: now, Type: LogTypeConsume, Username: "alice", TokenName: "key-a", ModelName: "model-a", ChannelId: 1, Group: "group-a", Quota: 3000, PromptTokens: 30, CompletionTokens: 3},
		{CreatedAt: now, Type: LogTypeConsume, Username: "alice", TokenName: "key-a", ModelName: "model-a", ChannelId: 1, Group: "group-a", Quota: 0, PromptTokens: 5, CompletionTokens: 2},
		{CreatedAt: now, Type: LogTypeConsume, Username: "bob", TokenName: "key-b", ModelName: "model-b", ChannelId: 2, Group: "group-b", Quota: 7000, PromptTokens: 70, CompletionTokens: 7},
		{CreatedAt: 100, Type: LogTypeConsume, Username: "idle", Quota: 4000, PromptTokens: 40, CompletionTokens: 4},
		{CreatedAt: now, Type: LogTypeTopup, Username: "alice", TokenName: "key-a", ModelName: "model-a", ChannelId: 1, Group: "group-a", Quota: 900000, PromptTokens: 9000, CompletionTokens: 900},
	}
	require.NoError(t, db.Create(&logs).Error)

	cases := []struct {
		name         string
		start, end   int64
		username     string
		token, model string
		channel      int
		group        string
		want         Stat
	}{
		{name: "all consume logs", want: Stat{Quota: 44000, Rpm: 3, Tpm: 117}},
		{name: "historical quota with current rates", start: 100, end: 200, want: Stat{Quota: 34000, Rpm: 3, Tpm: 117}},
		{name: "start bound only", start: 201, want: Stat{Quota: 10000, Rpm: 3, Tpm: 117}},
		{name: "end bound only", end: 199, want: Stat{Quota: 14000, Rpm: 3, Tpm: 117}},
		{name: "recent quota", start: now - 1, end: now + 1, want: Stat{Quota: 10000, Rpm: 3, Tpm: 117}},
		{name: "self stats", username: "alice", want: Stat{Quota: 33000, Rpm: 2, Tpm: 40}},
		{name: "no recent traffic", username: "idle", want: Stat{Quota: 4000}},
		{name: "token filter", token: "key-b", want: Stat{Quota: 7000, Rpm: 1, Tpm: 77}},
		{name: "model filter", model: "model-b", want: Stat{Quota: 7000, Rpm: 1, Tpm: 77}},
		{name: "channel filter", channel: 2, want: Stat{Quota: 7000, Rpm: 1, Tpm: 77}},
		{name: "group filter", group: "group-b", want: Stat{Quota: 7000, Rpm: 1, Tpm: 77}},
		{name: "combined filters", username: "alice", token: "key-a", model: "model-a", channel: 1, group: "group-a", start: 100, end: 200, want: Stat{Quota: 30000, Rpm: 2, Tpm: 40}},
		{name: "conflicting filters", username: "alice", group: "group-b", want: Stat{}},
		{name: "no matches", username: "missing", want: Stat{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stat, err := SumUsedQuota(LogTypeUnknown, tc.start, tc.end, tc.model, tc.username, tc.token, tc.channel, tc.group)
			require.NoError(t, err)
			assert.Equal(t, tc.want, stat)
		})
	}
}
