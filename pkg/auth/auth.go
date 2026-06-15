// Package auth 提供密码哈希与后台 session 的 JWT 签发/校验。
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// SessionTTL 是 session JWT 的有效期。
const SessionTTL = 7 * 24 * time.Hour

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// sessionClaims 是 session JWT 的载荷：标准 RegisteredClaims(sub=用户名, exp)+ 角色。
type sessionClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// SignSession 用 HS256 + secret 签发一个 7 天有效的 session JWT（sub=username）。
func SignSession(secret []byte, username, role string) string {
	return signSession(secret, username, role, SessionTTL)
}

// signSession 内部版本，可指定 TTL（便于测试过期）。
func signSession(secret []byte, username, role string, ttl time.Duration) string {
	now := time.Now()
	claims := sessionClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "" // secret 为 []byte 时 HS256 不会失败；保险返回空串（解析端会拒）
	}
	return tok
}

// ParseSession 校验 JWT 签名与过期，返回 username/role。过期或签名不符均 ok=false。
func ParseSession(secret []byte, token string) (username, role string, ok bool) {
	var claims sessionClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid // 只接受 HMAC，拒绝 alg 混淆攻击
		}
		return secret, nil
	})
	if err != nil || !parsed.Valid {
		return "", "", false
	}
	return claims.Subject, claims.Role, true
}
