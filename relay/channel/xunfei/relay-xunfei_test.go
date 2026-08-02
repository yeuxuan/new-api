package xunfei

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestXunfeiMakeRequestReturnsHandshakeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "handshake rejected", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	_, _, err := xunfeiMakeRequest(ctx, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{}, "general", websocketURL, "app-id")
	require.Error(t, err)
}
