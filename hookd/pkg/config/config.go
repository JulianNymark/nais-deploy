package config

type Config struct {
	ListenAddress string
	LogFormat     string
	LogLevel      string
	WebhookURL    string
	ApplicationID int
	InstallID     int
	KeyFile       string
	VaultAddress  string
	VaultPath     string
	KafkaBrokers  []string
	KafkaTopic    string
}

func DefaultConfig() *Config {
	return &Config{
		ListenAddress: ":8080",
		LogFormat:     "text",
		LogLevel:      "debug",
		WebhookURL:    "https://hookd/events",
		ApplicationID: 0,
		InstallID:     0,
		KeyFile:       "private-key.pem",
		VaultAddress:  "http://localhost:8200",
		VaultPath:     "/cubbyhole/hookd",
		KafkaBrokers:  []string{"localhost:9092"},
		KafkaTopic:    "deploymentRequest",
	}
}
