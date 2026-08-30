package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestSupportsNativeTaskRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("submit selects channel by model", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(`{"model":"seedance-2.0-fast"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")

		request, shouldSelectChannel, err := getModelRequest(ctx)
		require.NoError(t, err)
		assert.True(t, shouldSelectChannel)
		assert.Equal(t, "seedance-2.0-fast", request.Model)
		assert.Equal(t, relayconstant.RelayModeVideoSubmit, ctx.GetInt("relay_mode"))
	})

	t.Run("fetch uses persisted task without selecting channel", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/task_public", nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: "task_public"}}

		request, shouldSelectChannel, err := getModelRequest(ctx)
		require.NoError(t, err)
		assert.False(t, shouldSelectChannel)
		assert.Empty(t, request.Model)
		assert.Equal(t, relayconstant.RelayModeVideoFetchByID, ctx.GetInt("relay_mode"))
	})
}
