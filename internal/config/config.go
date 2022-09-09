package config

const (
	statusAppNotFoundMessage       = "app not found"
	statusInProgressMessage        = "in progress"
	statusFailedMessage            = "failed"
	statusArgoCDUnavailableMessage = "argocd is unavailable"
	statusDeployedMessage          = "deployed"
)

type Config struct {
	StatusAppNotFoundMessage       string
	StatusInProgressMessage        string
	StatusFailedMessage            string
	StatusArgoCDUnavailableMessage string
	StatusDeployedMessage          string
}

func GetConfig() *Config {
	return &Config{
		StatusAppNotFoundMessage:       statusAppNotFoundMessage,
		StatusInProgressMessage:        statusInProgressMessage,
		StatusFailedMessage:            statusFailedMessage,
		StatusArgoCDUnavailableMessage: statusArgoCDUnavailableMessage,
		StatusDeployedMessage:          statusDeployedMessage,
	}
}
