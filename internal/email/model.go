package email

type SenderConfig struct {
	Label     string
	FromEmail string
	Mode      string
}

type EmailRequest struct {
	Name        string `json:"name"`
	Company     string `json:"company"`
	Email       string `json:"email"`
	SenderKey   string `json:"sender_key"`
	TemplateKey string `json:"template_key"`
	Role        string `json:"role"`
}

type EmailTemplate struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Subject  string `json:"subject"`
	UsesRole bool   `json:"uses_role"`
}

type EmailTemplateManifest struct {
	Templates map[string]EmailTemplateMeta `json:"templates"`
}

type EmailTemplateMeta struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	File    string `json:"file"`
	UsesRole bool  `json:"uses_role"`
}

type Provider interface {
	Send(fromEmail, toEmail, name, company, templateKey, role string) (string, error)
}