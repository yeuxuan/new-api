package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// EmailBindSetting 邮箱绑定奖励配置
type EmailBindSetting struct {
	Enabled bool `json:"enabled"`
	Quota   int  `json:"quota"`
}

var emailBindSetting = EmailBindSetting{
	Enabled: false,
	Quota:   0,
}

func init() {
	config.GlobalConfig.Register("email_bind_setting", &emailBindSetting)
}

func GetEmailBindSetting() *EmailBindSetting {
	return &emailBindSetting
}

func IsEmailBindRewardEnabled() bool {
	return emailBindSetting.Enabled && emailBindSetting.Quota > 0
}
