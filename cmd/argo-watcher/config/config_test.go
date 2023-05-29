package config

import (
	"testing"
)

const (
	StateTypePostgres = "postgres"
)

func TestNewServerConfig_NoEnvironment(t *testing.T) {
	_, err := NewServerConfig()

	if err.Error() != "variable STATE_TYPE must be one of [\"postgres\", \"in-memory\"]" {
		t.Fail()
	}
}

func TestNewServerConfig_Environment(t *testing.T) {
	t.Setenv("STATE_TYPE", StateTypePostgres)

	serverConfig, err := NewServerConfig()

	if err != nil {
		t.Fail()
	}

	if serverConfig.StateType != StateTypePostgres {
		t.Errorf("STATE_TYPE value should be \"%v\"", StateTypePostgres)
	}

	// default values
	if serverConfig.ArgoApiTimeout != "60" {
		t.Error("ARGO_API_TIMEOUT default value should be \"60\"")
	}

	if serverConfig.ArgoTimeout != "0" {
		t.Error("ARGO_TIMEOUT default value should be \"0\"")
	}

	if serverConfig.StaticFilePath != "static" {
		t.Error("STATIC_FILES_PATH default value should be \"static\"")
	}

	if serverConfig.LogLevel != "info" {
		t.Error("LOG_LEVEL default value should be \"info\"")
	}

	if serverConfig.Host != "0.0.0.0" {
		t.Error("HOST default value should be \"0.0.0.0\"")
	}

	if serverConfig.Port != "8080" {
		t.Error("PORT default value should be \"8080\"")
	}

	if serverConfig.DbHost != "localhost" {
		t.Error("DB_HOST default value should be \"localhost\"")
	}

	if serverConfig.DbPort != "5432" {
		t.Error("DB_PORT default value should be \"5432\"")
	}

	if serverConfig.DbMigrationsPath != "db/migrations" {
		t.Error("DB_MIGRATIONS_PATH default value should be \"db/migrations\"")
	}

	if serverConfig.SkipTlsVerify != "false" {
		t.Error("SKIP_TLS_VERIFY default value should be \"false\"")
	}
}

func TestGetRetryAttempts_Default(t *testing.T) {
	t.Setenv("STATE_TYPE", StateTypePostgres)

	serverConfig, err := NewServerConfig()

	if err != nil {
		t.Fail()
	}

	timeout := serverConfig.GetRetryAttempts()

	if timeout != 1 {
		t.Error("ServerConfig.argoTimeout with default value of ARGO_TIMEOUT should be 1")
	}
}

func TestGetRetryAttempts_60(t *testing.T) {
	t.Setenv("STATE_TYPE", StateTypePostgres)
	t.Setenv("ARGO_TIMEOUT", "60")

	serverConfig, err := NewServerConfig()

	if err != nil {
		t.Fail()
	}

	timeout := serverConfig.GetRetryAttempts()

	if timeout != 5 {
		t.Error("ServerConfig.argoTimeout with value 60 should be 5")
	}
}
