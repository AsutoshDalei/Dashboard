package email

import (
	"fmt"
	"strings"
)

type Service struct {
	configs map[string]SenderConfig
}

func NewService(universityEmail, personalEmail, defaultKey string) *Service {
	configs := map[string]SenderConfig{
		"university": {Label: "University Gmail (API)", FromEmail: universityEmail, Mode: "gmail_api"},
		"personal":   {Label: "Personal Gmail (SMTP)", FromEmail: personalEmail, Mode: "smtp"},
	}
	if defaultKey == "" {
		defaultKey = "university"
	}
	return &Service{configs: configs}
}

func (s *Service) GetConfig(senderKey string) (SenderConfig, error) {
	config, ok := s.configs[senderKey]
	if !ok {
		var supported []string
		for k := range s.configs {
			supported = append(supported, k)
		}
		return SenderConfig{}, fmt.Errorf("unsupported sender_key '%s'. Supported: %s", senderKey, strings.Join(supported, ", "))
	}
	if config.FromEmail == "" {
		return SenderConfig{}, fmt.Errorf("%s missing from_email", config.Label)
	}
	return config, nil
}

func (s *Service) Send(toEmail, name, company, senderKey, templateKey, role string, attachResume bool, provider Provider) (string, error) {
	config, err := s.GetConfig(senderKey)
	if err != nil {
		return "", err
	}

	senderLabel, err := provider.Send(config.FromEmail, toEmail, name, company, templateKey, role, attachResume)
	if err != nil {
		return config.Label, fmt.Errorf("send failed: %w", err)
	}

	return senderLabel, nil
}