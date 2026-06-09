package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/ipaynow"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	PaymentMethodIPayNow = "ipaynow"

	iPayNowOrderTimeoutSec = 600
	iPayNowOrderName       = "TUC"
)

// IPayNowPayRequest 前端发起 iPayNow 下单的请求参数。
type IPayNowPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

// IPayNowAdaptor 保持与 Stripe/Creem 一致的 Adaptor 语义，方便将来扩展。
type IPayNowAdaptor struct{}

var iPayNowAdaptor = &IPayNowAdaptor{}

func getIPayNowMinTopup() int64 {
	minTopup := operation_setting.IPayNowMinTopUp
	if minTopup <= 0 {
		minTopup = 1
	}
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}

// IsIPayNowEnabled 是否已配置 iPayNow 支付
func IsIPayNowEnabled() bool {
	return operation_setting.IPayNowAppId != "" && operation_setting.IPayNowAppKey != ""
}

// RequestIPayNowPay 前端拉起 iPayNow 聚合动态码支付，返回可直接生成二维码的 URL。
func RequestIPayNowPay(c *gin.Context) {
	var req IPayNowPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	iPayNowAdaptor.RequestPay(c, &req)
}

func RequestIPayNowAmount(c *gin.Context) {
	var req IPayNowPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	iPayNowAdaptor.RequestAmount(c, &req)
}

func (*IPayNowAdaptor) RequestAmount(c *gin.Context, req *IPayNowPayRequest) {
	if req.Amount < getIPayNowMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getIPayNowMinTopup())})
		return
	}

	userId := c.GetInt("id")
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64), "group_ratio": topupGroupRatio})
}

func (*IPayNowAdaptor) RequestPay(c *gin.Context, req *IPayNowPayRequest) {
	if !requirePaymentCompliance(c) {
		return
	}
	if req.PaymentMethod != PaymentMethodIPayNow {
		c.JSON(200, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	if !IsIPayNowEnabled() {
		c.JSON(200, gin.H{"message": "error", "data": "当前管理员未配置 iPayNow 支付"})
		return
	}
	if req.Amount < getIPayNowMinTopup() {
		c.JSON(200, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getIPayNowMinTopup())})
		return
	}

	userId := c.GetInt("id")
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(200, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	// 以"分"为单位向 iPayNow 提交
	amountFen := decimal.NewFromFloat(payMoney).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if amountFen <= 0 {
		c.JSON(200, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	tradeNo := fmt.Sprintf("IPN%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	if len(tradeNo) > 40 {
		tradeNo = tradeNo[:40]
	}

	// 兑换展示量（tokens 模式下存储为美金等值）
	storedAmount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		storedAmount = decimal.NewFromInt(req.Amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}

	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          storedAmount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodIPayNow,
		PaymentProvider: model.PaymentProviderIPayNow,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		common.SysError("iPayNow 创建充值订单失败: " + err.Error())
		c.JSON(200, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	notifyURL := service.GetCallbackAddress() + "/api/ipaynow/notify"

	client := ipaynow.NewClient(operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey)
	resp, err := client.UnifiedOrder(&ipaynow.UnifiedOrderRequest{
		MhtOrderNo:      tradeNo,
		MhtOrderName:    fmt.Sprintf("%s%d", iPayNowOrderName, req.Amount),
		MhtOrderAmt:     amountFen,
		MhtOrderDetail:  fmt.Sprintf("%s%d", iPayNowOrderName, req.Amount),
		MhtOrderTimeOut: iPayNowOrderTimeoutSec,
		MhtOrderStartTs: ipaynow.FormatStartTime(time.Now()),
		NotifyURL:       notifyURL,
	})
	if err != nil {
		common.SysError("iPayNow 下单失败: " + err.Error())
		topUp.Status = common.TopUpStatusExpired
		_ = topUp.Update()
		c.JSON(200, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	qrURL := ipaynow.DecodeTN(resp.TN)
	if qrURL == "" {
		common.SysError("iPayNow 响应未返回 tn 字段: " + common.GetJsonString(resp))
		topUp.Status = common.TopUpStatusExpired
		_ = topUp.Update()
		c.JSON(200, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	c.JSON(200, gin.H{
		"message": "success",
		"data": gin.H{
			"trade_no": tradeNo,
			"qr_url":   qrURL,
		},
	})
}

// IPayNowNotify 接收 iPayNow 服务端异步通知（N001），校验签名并完成充值。
// 协议要求：
// - 从 HTTP body 流式读取参数；
// - 处理成功返回字符串 "success=Y"。
func IPayNowNotify(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		common.SysError("iPayNow 回调读取 body 失败: " + err.Error())
		c.String(200, "success=N")
		return
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		common.SysError("iPayNow 回调解析参数失败: " + err.Error())
		c.String(200, "success=N")
		return
	}
	if len(values) == 0 {
		common.SysError("iPayNow 回调参数为空")
		c.String(200, "success=N")
		return
	}

	params := make(map[string]string, len(values))
	for k := range values {
		params[k] = values.Get(k)
	}

	if !isIPayNowWebhookEnabled() {
		common.SysError("iPayNow 回调失败：未配置 appId/appKey 或未确认支付合规")
		c.String(200, "success=N")
		return
	}

	if !ipaynow.Verify(params, operation_setting.IPayNowAppKey) {
		common.SysError("iPayNow 回调签名校验失败: " + string(body))
		c.String(200, "success=N")
		return
	}

	tradeNo := params["mhtOrderNo"]
	if tradeNo == "" {
		common.SysError("iPayNow 回调缺少 mhtOrderNo")
		c.String(200, "success=N")
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		common.SysError("iPayNow 回调未找到订单: " + tradeNo)
		c.String(200, "success=N")
		return
	}

	// 幂等：已成功的订单直接应答
	if topUp.Status == common.TopUpStatusSuccess {
		c.String(200, "success=Y")
		return
	}

	if topUp.Status != common.TopUpStatusPending {
		common.SysError(fmt.Sprintf("iPayNow 回调订单状态异常: %s, 当前状态: %s", tradeNo, topUp.Status))
		c.String(200, "success=N")
		return
	}

	if params["transStatus"] != ipaynow.TransStatusSuccess {
		common.SysError(fmt.Sprintf("iPayNow 回调非成功状态: tradeNo=%s, transStatus=%s", tradeNo, params["transStatus"]))
		c.String(200, "success=Y")
		return
	}

	// 金额校验：mhtOrderAmt 单位为分，与本地 Money * 100 严格相等
	remoteAmt, parseErr := strconv.ParseInt(params["mhtOrderAmt"], 10, 64)
	if parseErr != nil {
		common.SysError("iPayNow 回调金额解析失败: " + params["mhtOrderAmt"])
		c.String(200, "success=N")
		return
	}
	localAmt := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if remoteAmt != localAmt {
		common.SysError(fmt.Sprintf("iPayNow 回调金额不一致: tradeNo=%s, remote=%d, local=%d", tradeNo, remoteAmt, localAmt))
		c.String(200, "success=N")
		return
	}

	if err := completeIPayNowTopUp(topUp, c.ClientIP()); err != nil {
		common.SysError("iPayNow 充值落库失败: " + err.Error())
		c.String(200, "success=N")
		return
	}

	c.String(200, "success=Y")
}

func completeIPayNowTopUp(topUp *model.TopUp, callerIp string) error {
	if topUp == nil {
		return errors.New("空订单")
	}

	// 防跨网关：iPayNow 回调只允许完成 iPayNow 订单。
	// 以 PaymentMethod 判定（部署前的在途订单 PaymentProvider 可能为空，
	// 但 PaymentMethod 始终为 ipaynow），既挡住冒充又不误伤历史订单。
	if topUp.PaymentMethod != PaymentMethodIPayNow {
		return fmt.Errorf("订单支付方式不匹配: trade_no=%s payment_method=%s", topUp.TradeNo, topUp.PaymentMethod)
	}

	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = time.Now().Unix()
	if err := topUp.Update(); err != nil {
		return fmt.Errorf("更新订单失败: %w", err)
	}

	dAmount := decimal.NewFromInt(topUp.Amount)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
	if quotaToAdd <= 0 {
		return fmt.Errorf("计算额度失败: amount=%d", topUp.Amount)
	}

	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		return fmt.Errorf("增加用户额度失败: %w", err)
	}

	model.RecordTopupLogWithQuota(topUp.UserId,
		fmt.Sprintf("使用聚合动态码充值成功，充值金额: %v，支付金额：%.2f",
			logger.LogQuota(quotaToAdd), topUp.Money),
		quotaToAdd, callerIp, topUp.PaymentMethod, "ipaynow")
	return nil
}

// ipaynowQueryCooldown 限制对 iPayNow MQ002 的调用频率，tradeNo -> 最近一次查询 unix 秒
var ipaynowQueryCooldown sync.Map

// ipaynowActiveQueryMinDelaySec 下单后首次 MQ002 最小延迟（秒）；给 iPayNow 通道到账留一点时间
const ipaynowActiveQueryMinDelaySec int64 = 2

// ipaynowActiveQueryCooldownSec 每个 tradeNo 的 MQ002 调用冷却间隔（秒）；
// 与前端 3s 轮询对齐，意味着几乎每次轮询都会调 MQ002，延迟≈通道到账耗时
const ipaynowActiveQueryCooldownSec int64 = 3

// QueryIPayNowOrder 前端 QR 弹窗轮询订单状态；
// 若本地仍为 pending，异步补偿查询 iPayNow（MQ002），防止 notify 丢失导致订单卡住。
func QueryIPayNowOrder(c *gin.Context) {
	tradeNo := c.Param("trade_no")
	if tradeNo == "" {
		common.ApiErrorMsg(c, "未提供订单号")
		return
	}

	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}
	if topUp.UserId != c.GetInt("id") {
		common.ApiErrorMsg(c, "无权访问该订单")
		return
	}

	// notify 补偿：本地仍 pending 且 iPayNow 已启用时，主动调 MQ002
	if topUp.Status == common.TopUpStatusPending && isIPayNowWebhookEnabled() {
		if tryActiveQueryIPayNow(tradeNo) {
			if refreshed := model.GetTopUpByTradeNo(tradeNo); refreshed != nil {
				topUp = refreshed
			}
		}
	}

	common.ApiSuccess(c, gin.H{
		"status":   topUp.Status,
		"money":    topUp.Money,
		"trade_no": topUp.TradeNo,
	})
}

// tryActiveQueryIPayNow 带冷却地调 MQ002 查询上游订单状态，若成功则落库补偿。
// 返回 true 表示本次调用触发了订单状态变更（或订单已在成功态）。
func tryActiveQueryIPayNow(tradeNo string) bool {
	now := time.Now().Unix()
	if last, ok := ipaynowQueryCooldown.Load(tradeNo); ok {
		if last.(int64)+ipaynowActiveQueryCooldownSec > now {
			return false
		}
	}

	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		return false
	}
	// 订单创建后先给 notify 机会，避免刚下单就打 iPayNow
	if now-topUp.CreateTime < ipaynowActiveQueryMinDelaySec {
		return false
	}
	ipaynowQueryCooldown.Store(tradeNo, now)

	client := ipaynow.NewClient(operation_setting.IPayNowAppId, operation_setting.IPayNowAppKey)
	resp, err := client.QueryOrder(&ipaynow.QueryOrderRequest{MhtOrderNo: tradeNo})
	if err != nil {
		common.SysError("iPayNow MQ002 查询失败: " + err.Error())
		return false
	}
	if resp == nil {
		common.SysError("iPayNow MQ002 空响应: " + tradeNo)
		return false
	}
	common.SysLog(fmt.Sprintf("iPayNow MQ002 响应: tradeNo=%s, responseCode=%s, transStatus=%s, body=%s",
		tradeNo, resp.ResponseCode, resp.TransStatus, resp.RawBody))
	if resp.TransStatus != ipaynow.TransStatusSuccess {
		return false
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	topUp = model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		return false
	}
	if topUp.Status == common.TopUpStatusSuccess {
		return true
	}
	if topUp.Status != common.TopUpStatusPending {
		return false
	}

	// 金额校验（上游可能不返回 mhtOrderAmt，留空时跳过严格校验）
	if resp.MhtOrderAmt != "" {
		remoteAmt, parseErr := strconv.ParseInt(resp.MhtOrderAmt, 10, 64)
		if parseErr == nil {
			localAmt := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
			if remoteAmt != localAmt {
				common.SysError(fmt.Sprintf("iPayNow MQ002 金额不一致: tradeNo=%s, remote=%d, local=%d", tradeNo, remoteAmt, localAmt))
				return false
			}
		}
	}

	if err := completeIPayNowTopUp(topUp, ""); err != nil {
		common.SysError("iPayNow MQ002 补偿落库失败: " + err.Error())
		return false
	}
	common.SysLog("iPayNow MQ002 补偿成功: " + tradeNo)
	return true
}
