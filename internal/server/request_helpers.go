package server

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// validateToken validates the incoming request using the configured authentication strategies.
// When allowedAuthStrategy is empty, the validation delegates to the default authenticator,
// which returns the last validation error when no strategies succeed. When allowedAuthStrategy
// is provided, validation is restricted to that specific strategy header and the last error from
// that strategy is returned if validation ultimately fails.
func (env *Env) validateToken(c *gin.Context, allowedAuthStrategy string) (bool, error) {
	if allowedAuthStrategy == "" {
		return env.authenticator.Validate(c.Request)
	}

	return env.validateAllowedStrategy(c, allowedAuthStrategy)
}

// validateAllowedStrategy enforces validation against the single allowed authentication strategy header.
// while keeping track of the last validation error produced by that strategy.
func (env *Env) validateAllowedStrategy(c *gin.Context, allowedStrategyHeader string) (bool, error) {
	var lastErr error

	for header, strategy := range env.strategies {
		token := c.GetHeader(header)
		if token == "" {
			continue
		}

		if header != allowedStrategyHeader {
			log.Debug().Msgf("Authorization strategy %s is not allowed for [%s] %s endpoint",
				header,
				c.Request.Method,
				c.Request.URL,
			)
			continue
		}

		log.Debug().Msgf("Using %s strategy for [%s] %s", header, c.Request.Method, c.Request.URL)

		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}

		valid, err := strategy.Validate(token)
		if err != nil {
			lastErr = err
		}
		if valid {
			return true, nil
		}
	}

	return false, lastErr
}

// hasAuthConfigured reports whether any authentication mechanism is configured and should be enforced.
func (env *Env) hasAuthConfigured() bool {
	return env.config.Keycloak.Enabled ||
		env.config.JWTSecret != "" ||
		env.config.DeployToken != ""
}

func parseTimestampOrDefault(value string, fallback float64) (float64, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func parseBoolOrDefault(value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseBool(value)
}
