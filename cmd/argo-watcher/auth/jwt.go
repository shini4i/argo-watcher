package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type JWTAuthService struct {
	secretKey string
}

func (j *JWTAuthService) Init(secretKey string) {
	j.secretKey = secretKey
}

func (j *JWTAuthService) Validate(tokenStr string) (bool, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.secretKey), nil
	})

	if err != nil {
		return false, err
	}

	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return false, fmt.Errorf("token is not valid")
	}

	return true, nil
}
