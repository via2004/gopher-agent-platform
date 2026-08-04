package auth

import (
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"strconv"
	"strings"
	"time"
)

type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewTokenManager(secret, issuer string, ttl time.Duration) (*TokenManager, error) {
	if ttl <= 0 {
		return nil, errors.New("JWT ttl must be positive")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("JWT issuer is required")
	}
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 bytes")
	}

	return &TokenManager{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
	}, nil
}

func (m *TokenManager) Issue(userID uint64) (string, error) {
	if userID == 0 {
		return "", errors.New("userID must be positive")
	}

	now := time.Now()

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatUint(userID, 10),
		Issuer:    m.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(m.secret)

	if err != nil {
		return "", fmt.Errorf("sign token error: %w", err)
	}

	return tokenString, nil
}

func (m *TokenManager) Verify(tokenString string) (uint64, error) {
	if strings.TrimSpace(tokenString) == "" {
		return 0, errors.New("token is empty")
	}

	claims := &jwt.RegisteredClaims{}

	// 解析 Token 并把 payload 放进 claims
	token, err := jwt.ParseWithClaims(
		tokenString, claims,
		// Key function 返回 m.secret：库用它重新计算 HS256 签名。
		func(token *jwt.Token) (any, error) {
			return m.secret, nil
		},

		// 禁止攻击者把 header 中的算法换成其他算法。
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),

		// 只接受我们服务签发的 Token。
		jwt.WithIssuer(m.issuer),

		// 没有 exp 也视为无效。
		jwt.WithExpirationRequired(),

		// 检查iat
		jwt.WithIssuedAt(),
	)

	if err != nil {
		return 0, fmt.Errorf("verify token error: %w", err)
	}

	if !token.Valid {
		return 0, errors.New("token is invalid")
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse token subject error: %w", err)
	}
	if userID == 0 {
		return 0, errors.New("token subject must be positive")
	}

	return userID, nil
}
