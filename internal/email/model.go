package email

type SenderConfig struct {
	Label     string
	FromEmail string
	Mode      string
}

type EmailRequest struct {
	Name      string `json:"name"`
	Company   string `json:"company"`
	Email     string `json:"email"`
	SenderKey string `json:"sender_key"`
}

type Provider interface {
	Send(fromEmail, toEmail, name, company string) (string, error)
}