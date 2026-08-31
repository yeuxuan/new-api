package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogStatsResponses(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousLogDB, previousDatabaseType := model.LOG_DB, common.LogDatabaseType()
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousDatabaseType)
		require.NoError(t, sqlDB.Close())
	})
	model.LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]model.Log{
		{CreatedAt: 100, Type: model.LogTypeConsume, Username: "alice", Quota: 500000, PromptTokens: 100},
		{CreatedAt: now, Type: model.LogTypeConsume, Username: "alice", Quota: 250000, PromptTokens: 10, CompletionTokens: 2},
		{CreatedAt: now, Type: model.LogTypeConsume, Username: "bob", TokenName: "key-b", ModelName: "model-b", ChannelId: 2, Quota: 1000000, PromptTokens: 20, CompletionTokens: 4},
		{CreatedAt: now, Type: model.LogTypeTopup, Username: "alice", Quota: 5000000},
	}).Error)

	cases := []struct {
		name    string
		url     string
		handler gin.HandlerFunc
		want    string
	}{
		{
			name: "admin historical usage and live rates",
			url:  "/api/log/stat?type=0&start_timestamp=1&end_timestamp=200", handler: GetLogsStat,
			want: `{"success":true,"message":"","data":{"quota":500000,"rpm":2,"tpm":36}}`,
		},
		{
			name: "admin query filters",
			url:  "/api/log/stat?username=bob&token_name=key-b&model_name=model-b&channel=2", handler: GetLogsStat,
			want: `{"success":true,"message":"","data":{"quota":1000000,"rpm":1,"tpm":24}}`,
		},
		{
			name: "self usage ignores requested username",
			url:  "/api/log/self/stat?username=bob", handler: GetLogsSelfStat,
			want: `{"success":true,"message":"","data":{"quota":750000,"rpm":1,"tpm":12}}`,
		},
		{
			name: "self usage with no matches",
			url:  "/api/log/self/stat?model_name=missing", handler: GetLogsSelfStat,
			want: `{"success":true,"message":"","data":{"quota":0,"rpm":0,"tpm":0}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.url, nil)
			c.Set("username", "alice")
			tc.handler(c)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}

	// The quota query still succeeds, but a failed rate query must not return partial statistics.
	require.NoError(t, db.Migrator().DropColumn(&model.Log{}, "prompt_tokens"))
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{name: "admin rate query failure", handler: GetLogsStat},
		{name: "self rate query failure", handler: GetLogsSelfStat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Set("username", "alice")
			tc.handler(c)

			assert.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool        `json:"success"`
				Message string      `json:"message"`
				Data    *model.Stat `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.NotEmpty(t, response.Message)
			assert.Nil(t, response.Data)
		})
	}
}
