package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]any)
	userId := c.GetInt("id")
	userGroup, _ := model.GetUserGroup(userId, false)
	// Users flagged by an admin with hide_pricing_info must not see group ratios.
	userSetting, _ := model.GetUserSetting(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			groupInfo := map[string]any{
				"desc": desc,
			}
			if !userSetting.HidePricingInfo {
				groupInfo["ratio"] = service.GetUserGroupRatio(userGroup, groupName)
			}
			usableGroups[groupName] = groupInfo
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		autoInfo := map[string]any{
			"desc": setting.GetUsableGroupDescription("auto"),
		}
		if !userSetting.HidePricingInfo {
			autoInfo["ratio"] = "auto"
		}
		usableGroups["auto"] = autoInfo
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
