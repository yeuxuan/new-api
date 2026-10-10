package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSEOForPath(t *testing.T) {
	t.Run("model detail", func(t *testing.T) {
		page := seoForPath("/pricing/claude-opus-4.8", "https://computetoken.ai", "Compute Token")

		assert.Equal(t, "Compute Token — claude-opus-4.8 API Pricing & Health | New API", page.title)
		assert.Equal(t, "https://computetoken.ai/pricing/claude-opus-4.8", page.canonical)
		assert.Equal(t, "index, follow", page.robots)
		assert.Contains(t, page.jsonLD, `"@type":"WebPage"`)
		assert.Contains(t, page.jsonLD, `"url":"https://computetoken.ai"`)
	})

	t.Run("localized landing page", func(t *testing.T) {
		page := seoForPath("/zh/claude-code-api/", "https://computetoken.ai", "Compute Token")

		assert.Equal(t, "zh-CN", page.lang)
		assert.Equal(t, "https://computetoken.ai/zh/claude-code-api", page.canonical)
		assert.Equal(t, "https://computetoken.ai/claude-code-api", page.alternates["en"])
	})

	t.Run("private app route", func(t *testing.T) {
		for _, path := range []string{
			"/dashboard", "/users", "/users/42", "/security", "/subscriptions",
			"/redemption-codes", "/task-plugins", "/task-plugins/plugin-id",
			"/system-info", "/playground", "/chat", "/chat2link",
		} {
			page := seoForPath(path, "https://computetoken.ai", "Compute Token")
			assert.Equal(t, "noindex, nofollow", page.robots, path)
		}
	})

	t.Run("model management stays private while model docs remain indexable", func(t *testing.T) {
		for _, path := range []string{"/models", "/models/metadata", "/models/pricing", "/models/vendors", "/system-settings/models"} {
			assert.Equal(t, "noindex, nofollow", seoForPath(path, "https://computetoken.ai", "Compute Token").robots, path)
		}
		page := seoForPath("/pricing/%5BV2%5Dseedance-2.0", "https://computetoken.ai", "Compute Token")
		assert.Equal(t, "index, follow", page.robots)
		assert.Contains(t, page.title, "[V2]seedance-2.0")
		assert.Equal(t, "https://computetoken.ai/pricing/%5BV2%5Dseedance-2.0", page.canonical)
	})
}

func TestRobotsPreservesPublicDocumentationAndDisallowsModelManagement(t *testing.T) {
	original := system_setting.ServerAddress
	system_setting.ServerAddress = "https://computetoken.ai"
	t.Cleanup(func() { system_setting.ServerAddress = original })
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	serveRobots(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Disallow: /models/\n")
	assert.NotContains(t, recorder.Body.String(), "Disallow: /pricing")
	assert.Contains(t, recorder.Body.String(), "Sitemap: https://computetoken.ai/sitemap.xml")
}

func TestRenderSEOIndex(t *testing.T) {
	index := []byte(`<html lang="en"><head><!--seo:start--><title>New API</title><!--seo:end--></head></html>`)
	page := seoPage{
		title:       `Compute <Token>`,
		description: `Price "and" health`,
		canonical:   "https://computetoken.ai/pricing",
		robots:      "index, follow",
		lang:        "zh-CN",
		jsonLD:      `{"@type":"WebPage"}`,
	}

	result := string(renderSEOIndex(index, page))
	require.NotEmpty(t, result)
	assert.Contains(t, result, `<html lang="zh-CN">`)
	assert.Contains(t, result, `<title>Compute &lt;Token&gt;</title>`)
	assert.Contains(t, result, `content="Price &#34;and&#34; health"`)
	assert.Contains(t, result, `rel="canonical" href="https://computetoken.ai/pricing"`)
	assert.Equal(t, 1, strings.Count(result, "<!--seo:start-->"))
}

func TestSiteBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = original })

	system_setting.ServerAddress = "https://computetoken.ai/app/"
	request := httptest.NewRequest(http.MethodGet, "http://internal/", nil)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	assert.Equal(t, "https://computetoken.ai", siteBaseURL(context))
}

func TestWebFallbackRejectsMissingBuildFilesAndPreservesPages(t *testing.T) {
	previousMode := gin.Mode()
	previousWebLimit, previousStaticLimit := common.GlobalWebRateLimitEnable, common.GlobalStaticRateLimitEnable
	gin.SetMode(gin.TestMode)
	common.GlobalWebRateLimitEnable, common.GlobalStaticRateLimitEnable = false, false
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		common.GlobalWebRateLimitEnable, common.GlobalStaticRateLimitEnable = previousWebLimit, previousStaticLimit
	})

	engine := gin.New()
	SetWebRouter(engine, WebAssets{IndexPage: []byte(`<html><head><!--seo:start--><title>New API</title><!--seo:end--></head><body>dashboard-shell</body></html>`)}, func(*gin.Context) {})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/static/js/previous-build.js", http.StatusNotFound},
		{"/static/css/previous-build.css?version=old", http.StatusNotFound},
		{"/assets/missing.js", http.StatusNotFound},
		{"/dashboard", http.StatusOK},
		{"/pricing/claude-opus-4.8", http.StatusOK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, response.Code)
			if tc.status == http.StatusOK {
				assert.Contains(t, response.Body.String(), "dashboard-shell")
				assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
			} else {
				assert.NotContains(t, response.Body.String(), "dashboard-shell")
			}
		})
	}
}
