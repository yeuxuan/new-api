package ipaynow

// Endpoint 与功能码（聚合动态码产品）
const (
	Endpoint = "https://pay.ipaynow.cn/"

	FuncUnifiedOrder = "WP001"
	FuncQueryOrder   = "MQ002"
	FuncNotify       = "N001"

	Version = "1.0.4"

	DeviceTypeAggregateQR = "20"
	OrderTypeConsume      = "05"
	CurrencyRMB           = "156"
	Charset               = "UTF-8"
	SignTypeMD5           = "MD5"
	OutputTypeURL         = "1"
	LimitPayAllowed       = "0"
)

// 响应码（§6.2 交易响应码表）
const (
	ResponseCodeSuccess = "A001"
	ResponseCodeFail    = "A002"
	ResponseCodeUnknown = "A003"
)

// 交易支付状态（§6.3 交易支付状态表）
const (
	TransStatusPending = "A00I"
	TransStatusSuccess = "A001"
	TransStatusFail    = "A002"
	TransStatusClosed  = "A006"
)

// UnifiedOrderRequest 下单请求（WP001）
type UnifiedOrderRequest struct {
	AppID            string
	MhtOrderNo       string
	MhtOrderName     string
	MhtOrderAmt      int64 // 单位：分
	MhtOrderDetail   string
	MhtOrderTimeOut  int // 秒
	MhtOrderStartTs  string
	NotifyURL        string
}

// UnifiedOrderResponse 下单响应（WP001 同步返回）
type UnifiedOrderResponse struct {
	FuncCode     string `form:"funcode"`
	Version      string `form:"version"`
	AppID        string `form:"appId"`
	ResponseCode string `form:"responseCode"`
	ResponseTime string `form:"responseTime"`
	ResponseMsg  string `form:"responseMsg"`
	MhtOrderNo   string `form:"mhtOrderNo"`
	TransStatus  string `form:"transStatus"`
	TN           string `form:"tn"`
	SignType     string `form:"signType"`
	Signature    string `form:"signature"`
}

// Success 判断响应是否受理成功
func (r *UnifiedOrderResponse) Success() bool {
	return r.ResponseCode == ResponseCodeSuccess
}

// QueryOrderRequest 订单查询请求（MQ002）
type QueryOrderRequest struct {
	MhtOrderNo string
}

// QueryOrderResponse 订单查询响应（MQ002 同步返回）
type QueryOrderResponse struct {
	FuncCode     string
	AppID        string
	ResponseCode string
	ResponseMsg  string
	MhtOrderNo   string
	TransStatus  string
	MhtOrderAmt  string
	Signature    string
}

// Success 判断响应是否受理成功
func (r *QueryOrderResponse) Success() bool {
	return r.ResponseCode == ResponseCodeSuccess
}
