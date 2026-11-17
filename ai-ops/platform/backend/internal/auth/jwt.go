package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// jwtHeader 固定 HS256，避免引入额外依赖。
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

type jwtPayload struct {
	Sub string `json:"sub"`
	Ver int    `json:"ver"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// signJWT 签发 HS256 令牌，sub 为用户 ID，ver 为 TokenVersion。
func signJWT(secret []byte, userID string, tokenVersion int, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("JWT 密钥未配置")
	}
	now := time.Now()
	payload, err := json.Marshal(jwtPayload{
		Sub: userID,
		Ver: tokenVersion,
		Iat: now.Unix(),
		Exp: now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	body := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + signBody(secret, body), nil
}

// parseJWT 校验签名与有效期，返回载荷。
func parseJWT(secret []byte, token string) (*jwtPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("令牌格式错误")
	}
	expected := signBody(secret, parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, fmt.Errorf("令牌签名无效")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("令牌载荷无效")
	}
	var p jwtPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("令牌载荷无效")
	}
	if p.Sub == "" {
		return nil, fmt.Errorf("令牌缺少用户标识")
	}
	if time.Now().Unix() >= p.Exp {
		return nil, fmt.Errorf("令牌已过期")
	}
	return &p, nil
}

func signBody(secret []byte, body string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
