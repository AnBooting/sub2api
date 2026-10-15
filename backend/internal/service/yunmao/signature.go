package yunmao

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// errInvalidTimestamp 表示回调时间戳无法解析。
var errInvalidTimestamp = errors.New("yunmao: invalid callback timestamp")

// SignHMACSHA256Hex 计算 HMAC-SHA256 的小写十六进制签名。
//
// 这是「签名算法已确定为 HMAC-SHA256」时才可用的工具函数；默认装配不使用它
// （默认是 PendingCallbackVerifier 的 fail closed 路径）。
func SignHMACSHA256Hex(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// constantTimeEqualHex 以恒定时间比较两个十六进制签名，避免时序侧信道。
func constantTimeEqualHex(actual, expected string) bool {
	decodedActual, errActual := hex.DecodeString(strings.TrimSpace(actual))
	decodedExpected, errExpected := hex.DecodeString(strings.TrimSpace(expected))
	if errActual != nil || errExpected != nil {
		return false
	}
	return hmac.Equal(decodedActual, decodedExpected)
}

func parseInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}
