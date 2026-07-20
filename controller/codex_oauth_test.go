package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartCodexOAuthStoresSessionBoundPKCEFlow(t *testing.T) {
	setupAuthFlowControllerTest(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/codex/oauth/start", nil)
	c.Set("id", 42)
	c.Set("session_id", "session-42")
	c.Set("auth_version", int64(3))
	c.Set("session_version", int64(2))

	StartCodexOAuth(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			AuthorizeURL string `json:"authorize_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	authorizeURL, err := url.Parse(response.Data.AuthorizeURL)
	require.NoError(t, err)
	state := authorizeURL.Query().Get("state")
	require.NotEmpty(t, state)
	require.NotEmpty(t, authorizeURL.Query().Get("code_challenge"))

	flow, err := model.GetAuthFlow(state, model.AuthFlowMatch{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "codex", Intent: model.AuthFlowIntentBind,
		UserId: 42, SessionId: "session-42",
	})
	require.NoError(t, err)
	var payload codexOAuthFlowPayload
	require.NoError(t, common.UnmarshalJsonStr(flow.Payload, &payload))
	assert.Zero(t, payload.ChannelID)
	assert.NotEmpty(t, payload.Verifier)
}

func TestStartCodexOAuthRequiresDashboardSession(t *testing.T) {
	setupAuthFlowControllerTest(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/codex/oauth/start", nil)

	StartCodexOAuth(c)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
