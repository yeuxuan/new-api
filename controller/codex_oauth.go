package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type codexOAuthCompleteRequest struct {
	Input string `json:"input"`
}

type codexOAuthFlowPayload struct {
	ChannelID int    `json:"channel_id"`
	Verifier  string `json:"verifier"`
}

func parseCodexAuthorizationInput(input string) (code string, state string, err error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", "", errors.New("empty input")
	}
	if strings.Contains(v, "#") {
		parts := strings.SplitN(v, "#", 2)
		code = strings.TrimSpace(parts[0])
		state = strings.TrimSpace(parts[1])
		return code, state, nil
	}
	if strings.Contains(v, "code=") {
		u, parseErr := url.Parse(v)
		if parseErr == nil {
			q := u.Query()
			code = strings.TrimSpace(q.Get("code"))
			state = strings.TrimSpace(q.Get("state"))
			return code, state, nil
		}
		q, parseErr := url.ParseQuery(v)
		if parseErr == nil {
			code = strings.TrimSpace(q.Get("code"))
			state = strings.TrimSpace(q.Get("state"))
			return code, state, nil
		}
	}

	code = v
	return code, "", nil
}

func StartCodexOAuth(c *gin.Context) {
	startCodexOAuthWithChannelID(c, 0)
}

func StartCodexOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	startCodexOAuthWithChannelID(c, channelID)
}

func startCodexOAuthWithChannelID(c *gin.Context, channelID int) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "dashboard session required"})
		return
	}
	if channelID > 0 {
		ch, err := model.GetChannelById(channelID, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if ch == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
			return
		}
		if ch.Type != constant.ChannelTypeCodex {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not Codex"})
			return
		}
	}

	verifier, challenge, err := service.GenerateCodexOAuthPKCEPair()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	payload, err := common.Marshal(codexOAuthFlowPayload{
		ChannelID: channelID,
		Verifier:  verifier,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	state, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  "codex",
		Intent:    model.AuthFlowIntentBind,
		UserId:    identity.UserID,
		SessionId: identity.SessionID,
		Payload:   string(payload),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	authorizeURL, err := service.BuildCodexOAuthAuthorizeURL(state, challenge)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"authorize_url": authorizeURL,
		},
	})
}

func CompleteCodexOAuth(c *gin.Context) {
	completeCodexOAuthWithChannelID(c, 0)
}

func CompleteCodexOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	completeCodexOAuthWithChannelID(c, channelID)
}

func completeCodexOAuthWithChannelID(c *gin.Context, channelID int) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "dashboard session required"})
		return
	}
	req := codexOAuthCompleteRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	code, state, err := parseCodexAuthorizationInput(req.Input)
	if err != nil {
		common.SysError("failed to parse codex authorization input: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析授权信息失败，请检查输入格式"})
		return
	}
	if strings.TrimSpace(code) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "missing authorization code"})
		return
	}
	if strings.TrimSpace(state) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "missing state in input"})
		return
	}

	channelProxy := ""
	if channelID > 0 {
		ch, err := model.GetChannelById(channelID, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if ch == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
			return
		}
		if ch.Type != constant.ChannelTypeCodex {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not Codex"})
			return
		}
		channelProxy = ch.GetSetting().Proxy
	}

	flowMatch := model.AuthFlowMatch{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  "codex",
		Intent:    model.AuthFlowIntentBind,
		UserId:    identity.UserID,
		SessionId: identity.SessionID,
	}
	pendingFlow, err := model.GetAuthFlow(state, flowMatch)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return
	}
	var flowPayload codexOAuthFlowPayload
	if err := common.UnmarshalJsonStr(pendingFlow.Payload, &flowPayload); err != nil ||
		flowPayload.ChannelID != channelID || strings.TrimSpace(flowPayload.Verifier) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow does not match channel"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	tokenRes, err := service.ExchangeCodexAuthorizationCodeWithProxy(ctx, code, flowPayload.Verifier, channelProxy)
	if err != nil {
		common.SysError("failed to exchange codex authorization code: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权码交换失败，请重试"})
		return
	}

	accountID, ok := service.ExtractCodexAccountIDFromJWT(tokenRes.AccessToken)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to extract account_id from access_token"})
		return
	}
	email, _ := service.ExtractEmailFromJWT(tokenRes.AccessToken)

	key := codex.OAuthKey{
		AccessToken:  tokenRes.AccessToken,
		RefreshToken: tokenRes.RefreshToken,
		AccountID:    accountID,
		LastRefresh:  time.Now().Format(time.RFC3339),
		Expired:      tokenRes.ExpiresAt.Format(time.RFC3339),
		Email:        email,
		Type:         "codex",
	}
	encoded, err := common.Marshal(key)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	_, err = model.ConsumeAuthFlowWithAction(state, flowMatch, func(tx *gorm.DB, _ *model.AuthFlow) error {
		if channelID == 0 {
			return nil
		}
		result := tx.Model(&model.Channel{}).Where("id = ?", channelID).Update("key", string(encoded))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if channelID > 0 {
		model.InitChannelCache()
		service.ResetProxyClientCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "saved",
			"data": gin.H{
				"channel_id":   channelID,
				"account_id":   accountID,
				"email":        email,
				"expires_at":   key.Expired,
				"last_refresh": key.LastRefresh,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "generated",
		"data": gin.H{
			"key":          string(encoded),
			"account_id":   accountID,
			"email":        email,
			"expires_at":   key.Expired,
			"last_refresh": key.LastRefresh,
		},
	})
}
