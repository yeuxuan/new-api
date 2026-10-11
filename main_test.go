package main

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestBatchUpdaterWithoutRedisUsesSynchronousUpdates(t *testing.T) {
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
	for _, tc := range []struct {
		name    string
		request string
		redis   bool
	}{
		{"requested without Redis", "true", false},
		{"disabled with Redis", "false", true},
		{"unset without Redis", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BATCH_UPDATE_ENABLED", tc.request)
			common.RedisEnabled = tc.redis
			common.BatchUpdateEnabled = true
			initBatchUpdater()
			assert.False(t, common.BatchUpdateEnabled, "authentication and quota reservations must retain the synchronous DB path")
		})
	}
}
