package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestVideoRouterRegistersNativeTaskCompatibilityRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, hasSubmit := routes[http.MethodPost+" /v1/tasks"]
	_, hasFetch := routes[http.MethodGet+" /v1/tasks/:task_id"]
	assert.True(t, hasSubmit)
	assert.True(t, hasFetch)
}
