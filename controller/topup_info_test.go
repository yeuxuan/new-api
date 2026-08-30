package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTopUpInfoExposesOnlineTopUpVisibility(t *testing.T) {
	previous := common.HideOnlineTopUp
	common.HideOnlineTopUp = true
	t.Cleanup(func() {
		common.HideOnlineTopUp = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	GetTopUpInfo(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			HideOnlineTopUp bool `json:"hide_online_topup"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.True(t, response.Data.HideOnlineTopUp)
}
