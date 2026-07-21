package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		page := seoForPath("/dashboard", "https://computetoken.ai", "Compute Token")
		assert.Equal(t, "noindex, nofollow", page.robots)
	})
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
