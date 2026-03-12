package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type ConversationLog struct {
	Id        int64  `json:"id" gorm:"primarykey;autoIncrement"`
	RequestId string `json:"request_id" gorm:"type:varchar(64);index"`
	UserId    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index;default:''"`
	TokenId   int    `json:"token_id" gorm:"index;default:0"`
	TokenName string `json:"token_name" gorm:"default:''"`
	ModelName string `json:"model_name" gorm:"index;default:''"`
	ChannelId int    `json:"channel_id" gorm:"index;default:0"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
	Messages  string `json:"messages" gorm:"type:text"`
	Response  string `json:"response" gorm:"type:text"`
	Ip        string `json:"ip" gorm:"default:''"`
}

var conversationLogQueue = make(chan *ConversationLog, 10000)

// StartConversationLogWorker 在 DB 初始化完成后调用
func StartConversationLogWorker() {
	go conversationLogWorker()
}

// EnqueueConversationLog 非阻塞投入队列，队列满则丢弃
func EnqueueConversationLog(log *ConversationLog) {
	select {
	case conversationLogQueue <- log:
	default:
		common.SysLog("conversation log queue full, dropping log")
	}
}

func conversationLogWorker() {
	defer func() {
		if r := recover(); r != nil {
			common.SysLog("conversation log worker panic, restarting: " + fmt.Sprintf("%v", r))
			go conversationLogWorker()
		}
	}()
	batch := make([]*ConversationLog, 0, 50)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case log := <-conversationLogQueue:
			batch = append(batch, log)
			if len(batch) >= 50 {
				flushConversationLogs(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flushConversationLogs(batch)
				batch = batch[:0]
			}
		}
	}
}

func flushConversationLogs(batch []*ConversationLog) {
	if err := DB.CreateInBatches(batch, 50).Error; err != nil {
		common.SysLog("failed to save conversation logs: " + err.Error())
	}
}
