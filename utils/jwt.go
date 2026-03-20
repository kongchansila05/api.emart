package utils

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload stored inside every JWT token.
type Claims struct {
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	RoleID uint   `json:"role_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// jwtSecret is read from the JWT_SECRET env variable with a fallback default.
// Always override this in production.
func jwtSecret() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("marketplace-default-secret-change-in-production")
}

// GenerateToken creates a signed JWT for the given user payload.
// The token expires after 24 hours.
func GenerateToken(userID, roleID uint, email, roleName string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		RoleID: roleID,
		Role:   roleName,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret())
}

// ParseToken validates a raw token string and returns the embedded claims.
// Returns an error when the token is malformed, expired, or has a bad signature.
func ParseToken(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("token is invalid")
	}
	return claims, nil
}
