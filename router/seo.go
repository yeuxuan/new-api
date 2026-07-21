package router

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

const (
	seoBlockStart = "<!--seo:start-->"
	seoBlockEnd   = "<!--seo:end-->"
)

type seoPage struct {
	title       string
	description string
	canonical   string
	robots      string
	lang        string
	jsonLD      string
	alternates  map[string]string
}

func siteBaseURL(c *gin.Context) string {
	configured := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	if parsed, err := url.Parse(configured); err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return parsed.Scheme + "://" + parsed.Host
	}

	scheme := "http"
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func brandedTitle(systemName, pageTitle string) string {
	name := strings.TrimSpace(systemName)
	if name == "" {
		name = "New API"
	}
	if pageTitle == "" {
		pageTitle = "Live Multi-Protocol AI API Gateway"
	}
	if name == "New API" {
		return name + " — " + pageTitle
	}
	return name + " — " + pageTitle + " | New API"
}

func seoForPath(path, baseURL, systemName string) seoPage {
	cleanPath := "/" + strings.Trim(strings.TrimSpace(path), "/")
	if cleanPath == "//" {
		cleanPath = "/"
	}
	page := seoPage{
		title:       brandedTitle(systemName, "Live Multi-Protocol AI API Gateway"),
		description: "Compare live AI model pricing and health, then call Claude, OpenAI, Gemini, and Responses APIs through one multi-protocol gateway.",
		canonical:   baseURL + cleanPath,
		robots:      "index, follow",
		lang:        "en",
	}

	switch {
	case cleanPath == "/":
		page.canonical = baseURL + "/"
	case cleanPath == "/pricing":
		page.title = brandedTitle(systemName, "Live AI Model Pricing & Health")
		page.description = "Compare current AI model input, output, and cache pricing alongside live latency, throughput, and success-rate signals."
	case strings.HasPrefix(cleanPath, "/pricing/"):
		modelName, err := url.PathUnescape(strings.TrimPrefix(cleanPath, "/pricing/"))
		if err != nil || strings.TrimSpace(modelName) == "" {
			modelName = "AI Model"
		}
		page.title = brandedTitle(systemName, modelName+" API Pricing & Health")
		page.description = fmt.Sprintf("View %s API pricing, supported protocols, capabilities, latency, throughput, and recent success-rate data.", modelName)
	case cleanPath == "/claude-code-api":
		page.title = brandedTitle(systemName, "Claude Code API Proxy with Live Pricing")
		page.description = "Connect Claude Code to a multi-protocol API gateway, compare supported Claude model prices, and inspect live model health before you build."
		page.alternates = map[string]string{
			"en":        baseURL + "/claude-code-api",
			"zh-CN":     baseURL + "/zh/claude-code-api",
			"x-default": baseURL + "/claude-code-api",
		}
	case cleanPath == "/zh/claude-code-api":
		page.title = brandedTitle(systemName, "Claude Code API 中转、价格与健康状态")
		page.description = "将 Claude Code 接入多协议 AI API 网关，比较 Claude 模型实时价格、支持协议、延迟与成功率。"
		page.lang = "zh-CN"
		page.alternates = map[string]string{
			"en":        baseURL + "/claude-code-api",
			"zh-CN":     baseURL + "/zh/claude-code-api",
			"x-default": baseURL + "/claude-code-api",
		}
	case cleanPath == "/about":
		page.title = brandedTitle(systemName, "About the AI API Gateway")
		page.description = "Learn how this New API-powered gateway unifies model access, billing, routing, and operational visibility."
	case cleanPath == "/rankings":
		page.title = brandedTitle(systemName, "AI Model Usage Rankings")
		page.description = "Explore the AI models developers use most across the gateway, with current request and token activity."
	case cleanPath == "/privacy-policy":
		page.title = brandedTitle(systemName, "Privacy Policy")
	case cleanPath == "/user-agreement":
		page.title = brandedTitle(systemName, "User Agreement")
	default:
		if isPrivateWebPath(cleanPath) {
			page.robots = "noindex, nofollow"
		}
	}

	page.jsonLD = buildWebPageJSONLD(page, systemName)
	return page
}

func isPrivateWebPath(path string) bool {
	privatePrefixes := []string{
		"/dashboard", "/console", "/wallet", "/profile", "/usage-logs",
		"/system-settings", "/channels", "/keys", "/logs", "/sign-in", "/sign-up",
	}
	for _, prefix := range privatePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func buildWebPageJSONLD(page seoPage, systemName string) string {
	name := strings.TrimSpace(systemName)
	if name == "" {
		name = "New API"
	}
	siteURL := page.canonical
	if parsed, err := url.Parse(page.canonical); err == nil && parsed.Host != "" {
		siteURL = parsed.Scheme + "://" + parsed.Host
	}
	payload := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebPage",
		"name":        page.title,
		"description": page.description,
		"url":         page.canonical,
		"isPartOf": map[string]any{
			"@type": "WebSite",
			"name":  name,
			"url":   siteURL,
		},
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(data), "</", "<\\/")
}

func renderSEOIndex(indexPage []byte, page seoPage) []byte {
	var head strings.Builder
	head.WriteString(seoBlockStart)
	head.WriteString("\n<title>")
	head.WriteString(html.EscapeString(page.title))
	head.WriteString("</title>")
	writeMeta := func(attribute, key, value string) {
		head.WriteString("\n<meta ")
		head.WriteString(attribute)
		head.WriteString("=\"")
		head.WriteString(key)
		head.WriteString("\" content=\"")
		head.WriteString(html.EscapeString(value))
		head.WriteString("\" />")
	}
	writeMeta("name", "title", page.title)
	writeMeta("name", "description", page.description)
	writeMeta("name", "robots", page.robots)
	writeMeta("property", "og:type", "website")
	writeMeta("property", "og:title", page.title)
	writeMeta("property", "og:description", page.description)
	writeMeta("property", "og:url", page.canonical)
	writeMeta("name", "twitter:card", "summary")
	writeMeta("name", "twitter:title", page.title)
	writeMeta("name", "twitter:description", page.description)
	head.WriteString("\n<link rel=\"canonical\" href=\"")
	head.WriteString(html.EscapeString(page.canonical))
	head.WriteString("\" />")

	if len(page.alternates) > 0 {
		languages := make([]string, 0, len(page.alternates))
		for language := range page.alternates {
			languages = append(languages, language)
		}
		sort.Strings(languages)
		for _, language := range languages {
			head.WriteString("\n<link rel=\"alternate\" hreflang=\"")
			head.WriteString(html.EscapeString(language))
			head.WriteString("\" href=\"")
			head.WriteString(html.EscapeString(page.alternates[language]))
			head.WriteString("\" />")
		}
	}
	if page.jsonLD != "" {
		head.WriteString("\n<script type=\"application/ld+json\">")
		head.WriteString(page.jsonLD)
		head.WriteString("</script>")
	}
	head.WriteString("\n")
	head.WriteString(seoBlockEnd)

	start := bytes.Index(indexPage, []byte(seoBlockStart))
	end := bytes.Index(indexPage, []byte(seoBlockEnd))
	if start >= 0 && end > start {
		end += len(seoBlockEnd)
		result := make([]byte, 0, len(indexPage)+head.Len())
		result = append(result, indexPage[:start]...)
		result = append(result, head.String()...)
		result = append(result, indexPage[end:]...)
		return bytes.Replace(result, []byte(`<html lang="en">`), []byte(`<html lang="`+html.EscapeString(page.lang)+`">`), 1)
	}

	closingHead := bytes.Index(indexPage, []byte("</head>"))
	if closingHead < 0 {
		return indexPage
	}
	result := make([]byte, 0, len(indexPage)+head.Len())
	result = append(result, indexPage[:closingHead]...)
	result = append(result, head.String()...)
	result = append(result, indexPage[closingHead:]...)
	return bytes.Replace(result, []byte(`<html lang="en">`), []byte(`<html lang="`+html.EscapeString(page.lang)+`">`), 1)
}

func serveRobots(c *gin.Context) {
	baseURL := siteBaseURL(c)
	body := "User-agent: *\n" +
		"Allow: /\n" +
		"Disallow: /api/\n" +
		"Disallow: /v1/\n" +
		"Disallow: /dashboard/\n" +
		"Disallow: /console/\n" +
		"Disallow: /wallet/\n" +
		"Disallow: /profile/\n" +
		"Disallow: /usage-logs/\n" +
		"Disallow: /system-settings/\n" +
		"Disallow: /sign-in\n" +
		"Disallow: /sign-up\n" +
		"Sitemap: " + baseURL + "/sitemap.xml\n"
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func serveSitemap(c *gin.Context) {
	baseURL := siteBaseURL(c)
	paths := []string{
		"/", "/pricing", "/claude-code-api", "/zh/claude-code-api", "/about",
		"/rankings", "/privacy-policy", "/user-agreement",
	}
	for _, pricing := range model.GetPricing() {
		if modelName := strings.TrimSpace(pricing.ModelName); modelName != "" {
			paths = append(paths, "/pricing/"+url.PathEscape(modelName))
		}
	}
	sort.Strings(paths)

	var sitemap strings.Builder
	sitemap.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	sitemap.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, path := range paths {
		sitemap.WriteString("  <url><loc>")
		sitemap.WriteString(html.EscapeString(baseURL + path))
		sitemap.WriteString("</loc></url>\n")
	}
	sitemap.WriteString("</urlset>\n")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(sitemap.String()))
}
