package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestGetUserTotalSpendableQuota(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceCheckin, "2026-06-01", 2000, 0)
	if err != nil {
		t.Fatalf("create bonus: %v", err)
	}

	total, err := GetUserTotalSpendableQuota(1, "gpt-4")
	if err != nil {
		t.Fatalf("total: %v", err)
	}
	if total != 7000 {
		t.Fatalf("expected 7000, got %d", total)
	}

	restricted, err := GetUserTotalSpendableQuota(1, "claude-3")
	if err != nil {
		t.Fatalf("restricted: %v", err)
	}
	// bonus not allowed for claude-3 when whitelist is empty all models allowed - actually empty whitelist allows all
	if restricted != 7000 {
		t.Fatalf("expected 7000 with open whitelist, got %d", restricted)
	}
}

func TestPostConsumeQuotaUsesBonusFirst(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceEmailBind, "", 2500, 0)
	if err != nil {
		t.Fatalf("create bonus: %v", err)
	}

	relayInfo := &relaycommon.RelayInfo{
		UserId:          1,
		OriginModelName: "gpt-4",
		TokenId:         0,
		IsPlayground:    true,
	}

	if err := PostConsumeQuota(relayInfo, 3000, 0, false); err != nil {
		t.Fatalf("post consume: %v", err)
	}

	bonus, _ := model.GetUserBonusQuotaTotal(1)
	wallet, _ := model.GetUserQuota(1, true)
	if bonus != 0 || wallet != 4500 {
		t.Fatalf("after consume bonus=%d wallet=%d", bonus, wallet)
	}
}

func TestWalletFundingSettleRefundBonusFirst(t *testing.T) {
	setupWalletFundingTestDB(t)

	_, err := model.CreateBonusQuotaGrant(1, model.BonusQuotaSourceCheckin, "2026-06-01", 4000, 0)
	if err != nil {
		t.Fatalf("create bonus: %v", err)
	}

	w := &WalletFunding{userId: 1, modelName: "gpt-4"}
	if err := w.PreConsume(5000); err != nil {
		t.Fatalf("preconsume: %v", err)
	}
	if err := w.Settle(-2000); err != nil {
		t.Fatalf("settle: %v", err)
	}

	bonus, _ := model.GetUserBonusQuotaTotal(1)
	wallet, _ := model.GetUserQuota(1, true)
	if bonus != 2000 || wallet != 4000 {
		t.Fatalf("after partial refund bonus=%d wallet=%d", bonus, wallet)
	}
}
