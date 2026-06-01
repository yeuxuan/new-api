package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// BonusQuotaSetting 奖励额度共用策略（有效期 + 模型白名单）
type BonusQuotaSetting struct {
	ValidityDays  int    `json:"validity_days"`
	AllowedModels string `json:"allowed_models"`
}

var bonusQuotaSetting = BonusQuotaSetting{
	ValidityDays:  0,
	AllowedModels: "",
}

func init() {
	config.GlobalConfig.Register("bonus_quota_setting", &bonusQuotaSetting)
}

func GetBonusQuotaSetting() *BonusQuotaSetting {
	return &bonusQuotaSetting
}

func (s *BonusQuotaSetting) GetAllowedModelsList() []string {
	if s.AllowedModels == "" {
		return nil
	}
	parts := strings.Split(s.AllowedModels, ",")
	models := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			models = append(models, p)
		}
	}
	return models
}

func IsModelAllowedForBonusQuota(modelName string) bool {
	models := bonusQuotaSetting.GetAllowedModelsList()
	if len(models) == 0 {
		return true
	}
	for _, m := range models {
		if m == modelName {
			return true
		}
	}
	return false
}

func CalcBonusQuotaExpiresAt(now int64) int64 {
	days := bonusQuotaSetting.ValidityDays
	if days <= 0 {
		return 0
	}
	return now + int64(days)*86400
}
