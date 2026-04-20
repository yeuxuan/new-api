package ipaynow

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client 封装 iPayNow 聚合动态码的服务端调用
type Client struct {
	AppID      string
	AppKey     string
	HTTPClient *http.Client
}

// NewClient 构造新的客户端
func NewClient(appID, appKey string) *Client {
	return &Client{
		AppID:  appID,
		AppKey: appKey,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// UnifiedOrder 调用 WP001 统一下单接口
func (c *Client) UnifiedOrder(req *UnifiedOrderRequest) (*UnifiedOrderResponse, error) {
	if c.AppID == "" || c.AppKey == "" {
		return nil, fmt.Errorf("ipaynow: appId 或 appKey 未配置")
	}
	if req == nil {
		return nil, fmt.Errorf("ipaynow: 请求为空")
	}

	timeout := req.MhtOrderTimeOut
	if timeout < 60 || timeout > 604800 {
		timeout = 3600
	}

	params := map[string]string{
		"funcode":           FuncUnifiedOrder,
		"version":           Version,
		"appId":             c.AppID,
		"mhtOrderNo":        req.MhtOrderNo,
		"mhtOrderName":      req.MhtOrderName,
		"mhtOrderType":      OrderTypeConsume,
		"mhtCurrencyType":   CurrencyRMB,
		"mhtOrderAmt":       strconv.FormatInt(req.MhtOrderAmt, 10),
		"mhtOrderDetail":    req.MhtOrderDetail,
		"mhtOrderTimeOut":   strconv.Itoa(timeout),
		"mhtOrderStartTime": req.MhtOrderStartTs,
		"notifyUrl":         req.NotifyURL,
		"mhtCharset":        Charset,
		"deviceType":        DeviceTypeAggregateQR,
		"mhtLimitPay":       LimitPayAllowed,
		"outputType":        OutputTypeURL,
		"mhtSignType":       SignTypeMD5,
	}

	params["mhtSignature"] = Sign(params, c.AppKey)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	httpReq, err := http.NewRequest(http.MethodPost, Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ipaynow: 构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ipaynow: 发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ipaynow: 读取响应失败: %w", err)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("ipaynow: 解析响应失败: %w, body=%s", err, string(body))
	}

	out := &UnifiedOrderResponse{
		FuncCode:     values.Get("funcode"),
		Version:      values.Get("version"),
		AppID:        values.Get("appId"),
		ResponseCode: values.Get("responseCode"),
		ResponseTime: values.Get("responseTime"),
		ResponseMsg:  values.Get("responseMsg"),
		MhtOrderNo:   values.Get("mhtOrderNo"),
		TransStatus:  values.Get("transStatus"),
		TN:           values.Get("tn"),
		SignType:     values.Get("signType"),
		Signature:    values.Get("signature"),
	}

	if !out.Success() {
		return out, fmt.Errorf("ipaynow: 受理失败 code=%s msg=%s", out.ResponseCode, out.ResponseMsg)
	}
	return out, nil
}

// DecodeTN 将同步返回的 tn（URL 编码）还原为可用于生成二维码的原始字符串。
func DecodeTN(tn string) string {
	if tn == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(tn); err == nil {
		return decoded
	}
	return tn
}

// FormatStartTime 按 iPayNow 要求格式化下单开始时间为 Asia/Shanghai 的 yyyyMMddHHmmss。
func FormatStartTime(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 环境缺少 tzdata 时退化到 UTC+8 定值
		loc = time.FixedZone("CST", 8*3600)
	}
	return t.In(loc).Format("20060102150405")
}
