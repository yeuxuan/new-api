package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type ClearCheckinQuotaRequest struct {
	StartDate string `json:"start_date" binding:"required"`
	EndDate   string `json:"end_date" binding:"required"`
	UserIds   []int  `json:"user_ids"`
}

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := model.GetUserCheckinStats(userId, month)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":   setting.Enabled,
			"min_quota": setting.MinQuota,
			"max_quota": setting.MaxQuota,
			"stats":     stats,
		},
	})
}

// DoCheckin 执行用户签到
func DoCheckin(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}

	userId := c.GetInt("id")

	checkin, err := model.UserCheckin(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "签到成功",
		"data": gin.H{
			"quota_awarded": checkin.QuotaAwarded,
			"checkin_date":  checkin.CheckinDate},
	})
}

func validateClearCheckinRequest(c *gin.Context) (*ClearCheckinQuotaRequest, bool) {
	var req ClearCheckinQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误：start_date 和 end_date 为必填项")
		return nil, false
	}
	_, err1 := time.Parse("2006-01-02", req.StartDate)
	_, err2 := time.Parse("2006-01-02", req.EndDate)
	if err1 != nil || err2 != nil {
		common.ApiErrorMsg(c, "日期格式错误，请使用 YYYY-MM-DD 格式")
		return nil, false
	}
	if req.StartDate > req.EndDate {
		common.ApiErrorMsg(c, "开始日期不能大于结束日期")
		return nil, false
	}
	today := time.Now().Format("2006-01-02")
	if req.StartDate > today {
		common.ApiErrorMsg(c, "开始日期不能是未来日期")
		return nil, false
	}
	if req.EndDate > today {
		common.ApiErrorMsg(c, "结束日期不能是未来日期")
		return nil, false
	}
	if len(req.UserIds) > 1000 {
		common.ApiErrorMsg(c, "单次最多指定 1000 个用户")
		return nil, false
	}
	return &req, true
}

// AdminPreviewClearCheckinQuota 预览清除签到额度
func AdminPreviewClearCheckinQuota(c *gin.Context) {
	req, ok := validateClearCheckinRequest(c)
	if !ok {
		return
	}

	items, err := model.PreviewClearCheckinQuota(req.StartDate, req.EndDate, req.UserIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if items == nil {
		items = []model.ClearCheckinPreviewItem{}
	}

	totalQuota := 0
	totalActualClear := 0
	for _, item := range items {
		totalQuota += item.CheckinQuota
		totalActualClear += item.ActualClear
	}

	common.ApiSuccess(c, gin.H{
		"users":              items,
		"affected_users":     len(items),
		"total_quota":        totalQuota,
		"total_actual_clear": totalActualClear,
	})
}

// AdminClearCheckinQuota 清除签到额度
func AdminClearCheckinQuota(c *gin.Context) {
	req, ok := validateClearCheckinRequest(c)
	if !ok {
		return
	}

	operatorName := c.GetString("username")
	if operatorName == "" {
		operatorName = "管理员"
	}

	affectedUsers, totalCleared, err := model.BatchClearCheckinQuota(req.StartDate, req.EndDate, req.UserIds, operatorName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"affected_users":     affectedUsers,
		"total_quota_cleared": totalCleared,
		"message": fmt.Sprintf("成功清除 %d 个用户的签到额度，共计 %s", affectedUsers, logger.LogQuota(totalCleared)),
	})
}
