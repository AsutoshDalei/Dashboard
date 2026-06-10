package email

import (
	"fmt"
	"net/smtp"
)

type SMTPProvider struct {
	fromEmail string
	password  string
}

func NewSMTPProvider(fromEmail, password string) *SMTPProvider {
	return &SMTPProvider{fromEmail: fromEmail, password: password}
}

func (p *SMTPProvider) Send(fromEmail, toEmail, name, company string) (string, error) {
	if fromEmail == "" {
		fromEmail = p.fromEmail
	}
	if fromEmail == "" || p.password == "" {
		return "", fmt.Errorf("SMTP not configured: check EMAIL and PASSWORD")
	}

	msg, err := buildMessage(fromEmail, toEmail, name, company)
	if err != nil {
		return "", fmt.Errorf("build message: %w", err)
	}

	auth := smtp.PlainAuth("", fromEmail, p.password, "smtp.gmail.com")
	if err := smtp.SendMail("smtp.gmail.com:587", auth, fromEmail, []string{toEmail}, msg); err != nil {
		return "", fmt.Errorf("smtp send: %w", err)
	}

	return "SMTP", nil
}