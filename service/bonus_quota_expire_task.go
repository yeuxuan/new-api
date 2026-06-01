package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	bonusQuotaExpireTickInterval = 1 * time.Minute
	bonusQuotaExpireBatchSize    = 300
)

var bonusQuotaExpireOnce sync.Once

func StartBonusQuotaExpireTask() {
	bonusQuotaExpireOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(bonusQuotaExpireTickInterval)
			defer ticker.Stop()
			runBonusQuotaExpireOnce()
			for range ticker.C {
				runBonusQuotaExpireOnce()
			}
		})
	})
}

func runBonusQuotaExpireOnce() {
	total := 0
	for {
		n, err := model.ExpireDueBonusQuotas(bonusQuotaExpireBatchSize)
		if err != nil {
			common.SysLog("bonus quota expire task failed: " + err.Error())
			return
		}
		if n == 0 {
			break
		}
		total += n
		if n < bonusQuotaExpireBatchSize {
			break
		}
	}
}
