package controller

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 匹配 content 中的额度金额，覆盖以下格式：
// "充值额度: ＄0.123456，..." / "充值金额: ¥0.123，..." / "充值额度: 500000，..."（Tokens模式）
// "获得额度 ＄0.001 额度" / "赠送 ＄0.001 额度"
var quotaLabelRe = regexp.MustCompile(`(?:充值(?:额度|金额)|获得额度|赠送)\s*[：:\s]\s*(\S+?)(?:[，,\s]|$)`)
var quotaCurrencyRe = regexp.MustCompile(`[\$＄¥¤]\s*([\d.]+)`)

const exportMaxRows = 10000

var logTypeNames = map[int]string{
	0: "Unknown",
	1: "Topup",
	2: "Consume",
	3: "Manage",
	4: "System",
	5: "Error",
	6: "Refund",
}

func getLogTypeName(t int) string {
	if name, ok := logTypeNames[t]; ok {
		return name
	}
	return "Unknown"
}

func setCSVHeaders(c *gin.Context, filename string) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
}

func newCSVWriter(c *gin.Context) *csv.Writer {
	// UTF-8 BOM for Excel compatibility — must be written before csv.NewWriter buffers anything
	_, _ = c.Writer.Write([]byte("\xEF\xBB\xBF"))
	return csv.NewWriter(c.Writer)
}

func flushCSVWriter(writer *csv.Writer) {
	writer.Flush()
	if err := writer.Error(); err != nil {
		common.SysLog("csv flush error: " + err.Error())
	}
}

// ExportAllLogsCSV streams the admin log export as CSV for the classic theme.
func ExportAllLogsCSV(c *gin.Context) {
	filter := logFilterFromQuery(c, true)
	startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filter.StartTimestamp = startTimestamp

	logs, _, err := model.GetAllLogs(filter, 0, exportMaxRows)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	setCSVHeaders(c, fmt.Sprintf("logs_export_%d.csv", time.Now().Unix()))
	writer := newCSVWriter(c)

	_ = writer.Write([]string{
		"Time", "Username", "TokenName", "ModelName", "Group", "Type",
		"PromptTokens", "CompletionTokens", "Quota", "UseTime",
		"Channel", "ChannelName", "IP", "RequestID", "IsStream",
	})

	for _, log := range logs {
		_ = writer.Write([]string{
			time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05"),
			log.Username,
			log.TokenName,
			log.ModelName,
			log.Group,
			getLogTypeName(log.Type),
			strconv.Itoa(log.PromptTokens),
			strconv.Itoa(log.CompletionTokens),
			strconv.Itoa(log.Quota),
			strconv.Itoa(log.UseTime),
			strconv.Itoa(log.ChannelId),
			log.ChannelName,
			log.Ip,
			log.RequestId,
			strconv.FormatBool(log.IsStream),
		})
	}

	flushCSVWriter(writer)
}

// ExportUserLogsCSV streams the caller's own log export as CSV for the classic
// theme.
func ExportUserLogsCSV(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser && !operation_setting.GetLogSetting().UserExportEnabled {
		common.ApiErrorT(c, "Log export is disabled")
		return
	}
	filter := logFilterFromQuery(c, false)
	startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filter.StartTimestamp = startTimestamp

	logs, _, err := model.GetUserLogs(c.GetInt("id"), filter, 0, exportMaxRows)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	setCSVHeaders(c, fmt.Sprintf("logs_export_%d.csv", time.Now().Unix()))
	writer := newCSVWriter(c)

	_ = writer.Write([]string{
		"Time", "TokenName", "ModelName", "Group", "Type",
		"PromptTokens", "CompletionTokens", "Quota", "UseTime",
		"RequestID", "IsStream",
	})

	for _, log := range logs {
		_ = writer.Write([]string{
			time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05"),
			log.TokenName,
			log.ModelName,
			log.Group,
			getLogTypeName(log.Type),
			strconv.Itoa(log.PromptTokens),
			strconv.Itoa(log.CompletionTokens),
			strconv.Itoa(log.Quota),
			strconv.Itoa(log.UseTime),
			log.RequestId,
			strconv.FormatBool(log.IsStream),
		})
	}

	flushCSVWriter(writer)
}

const exportBatchSize = 2000

func ExportQuotaLogs(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")

	tx := model.BuildQuotaLogQuery(logType, startTimestamp, endTimestamp, username)

	setCSVHeaders(c, fmt.Sprintf("quota_logs_export_%d.csv", time.Now().Unix()))
	writer := newCSVWriter(c)

	_ = writer.Write([]string{
		"Date", "Time", "UserID", "Username", "Group", "Type",
		"QuotaChange", "QuotaChangeUSD",
		"ModelName", "TokenName", "Details",
	})

	lastId := 0
	for {
		var logs []*model.Log
		query := tx.Session(&gorm.Session{})
		if lastId > 0 {
			query = query.Where("id < ?", lastId)
		}
		if err := query.Order("id desc").Limit(exportBatchSize).Find(&logs).Error; err != nil {
			common.SysLog("export quota logs batch error: " + err.Error())
			break
		}
		if len(logs) == 0 {
			break
		}

		for _, log := range logs {
			quotaChange := log.Quota
			if log.Type == model.LogTypeConsume {
				quotaChange = -abs(quotaChange)
			}

			var quotaChangeStr, quotaUSD string
			if quotaChange != 0 {
				quotaChangeStr = strconv.Itoa(quotaChange)
				quotaUSD = fmt.Sprintf("%.6f", float64(quotaChange)/common.QuotaPerUnit)
			} else {
				parsed := parseQuotaFromContent(log.Content)
				if parsed != "" {
					prefix := "+"
					if log.Type == model.LogTypeConsume {
						prefix = "-"
					}
					quotaChangeStr = prefix + parsed
					quotaUSD = prefix + parsed
				}
			}

			ts := time.Unix(log.CreatedAt, 0)
			_ = writer.Write([]string{
				ts.Format("2006-01-02"),
				ts.Format("2006-01-02 15:04:05"),
				strconv.Itoa(log.UserId),
				log.Username,
				log.Group,
				getLogTypeName(log.Type),
				quotaChangeStr,
				quotaUSD,
				log.ModelName,
				log.TokenName,
				log.Content,
			})
		}

		writer.Flush()
		lastId = logs[len(logs)-1].Id

		if len(logs) < exportBatchSize {
			break
		}
	}

	flushCSVWriter(writer)
}

func parseQuotaFromContent(content string) string {
	if m := quotaLabelRe.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	if m := quotaCurrencyRe.FindStringSubmatch(content); m != nil {
		return m[0]
	}
	return ""
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func ExportAllUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	order := c.Query("order")

	var users []*model.User
	var err error

	if keyword != "" || group != "" {
		users, err = model.SearchUsersForExport(keyword, group, order, exportMaxRows)
	} else {
		users, err = model.GetAllUsersForExport(order, exportMaxRows)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	setCSVHeaders(c, fmt.Sprintf("users_export_%d.csv", time.Now().Unix()))
	writer := newCSVWriter(c)

	_ = writer.Write([]string{
		"ID", "Username", "DisplayName", "Email", "Role", "Status",
		"Group", "Quota", "UsedQuota", "RequestCount", "Remark",
	})

	for _, user := range users {
		roleName := "Normal"
		switch user.Role {
		case common.RoleAdminUser:
			roleName = "Admin"
		case common.RoleRootUser:
			roleName = "SuperAdmin"
		}

		statusName := "Enabled"
		if user.DeletedAt.Valid {
			statusName = "Deleted"
		} else if user.Status == 2 {
			statusName = "Disabled"
		}

		_ = writer.Write([]string{
			strconv.Itoa(user.Id),
			user.Username,
			user.DisplayName,
			user.Email,
			roleName,
			statusName,
			user.Group,
			strconv.Itoa(user.Quota),
			strconv.Itoa(user.UsedQuota),
			strconv.Itoa(user.RequestCount),
			user.Remark,
		})
	}

	flushCSVWriter(writer)
}
