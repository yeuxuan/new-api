package ipaynow

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 参与签名时需要排除的字段（iPayNow §6.1：除签名串本身以外所有参数都参与签名）
var signIgnoredKeys = map[string]struct{}{
	"mhtSignature": {},
	"signature":    {},
}

// buildSignContent 生成待签名字符串：
// 字典升序，排除空值与签名字段，拼成 k1=v1&k2=v2...
func buildSignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" {
			continue
		}
		if _, skip := signIgnoredKeys[k]; skip {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

// Sign 按 iPayNow §6.1 签名规则生成 MD5 签名：
// MD5( buildSignContent(params) + "&" + MD5(appKey) )
func Sign(params map[string]string, appKey string) string {
	content := buildSignContent(params)
	keyDigest := common.Md5([]byte(appKey))
	return common.Md5([]byte(content + "&" + keyDigest))
}

// Verify 校验服务端发来的签名。签名字段来自 mhtSignature 或 signature。
func Verify(params map[string]string, appKey string) bool {
	expected := ""
	if s, ok := params["signature"]; ok && s != "" {
		expected = s
	} else if s, ok := params["mhtSignature"]; ok && s != "" {
		expected = s
	}
	if expected == "" {
		return false
	}
	return Sign(params, appKey) == expected
}
