package common

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultEndpointInfoIncludesOpenAIVideoTaskSubmission(t *testing.T) {
	info, ok := GetDefaultEndpointInfo(constant.EndpointTypeOpenAIVideo)
	require.True(t, ok)
	assert.Equal(t, "/v1/tasks", info.Path)
	assert.Equal(t, http.MethodPost, info.Method)
}
