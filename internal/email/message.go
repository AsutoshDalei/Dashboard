package email

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"pi_dashboard/pkg/mail"
)

type EmailTemplateData struct {
	Name    string
	Company string
}

func loadTemplateManifest() (*EmailTemplateManifest, error) {
	candidates := []string{"templates/email_templates.json", filepath.Join("..", "templates", "email_templates.json")}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var manifest EmailTemplateManifest
		if err := json.Unmarshal(b, &manifest); err != nil {
			return nil, fmt.Errorf("parse email_templates.json: %w", err)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("email_templates.json not found")
}

func renderSubject(subjectTmpl string, data EmailTemplateData) (string, error) {
	t, err := template.New("subject").Parse(subjectTmpl)
	if err != nil {
		return "", fmt.Errorf("parse subject template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render subject template: %w", err)
	}
	return buf.String(), nil
}

func resolveTemplatePath(templateKey string) (string, string, error) {
	manifest, err := loadTemplateManifest()
	if err != nil {
		return "", "", err
	}

	meta, ok := manifest.Templates[templateKey]
	if !ok {
		var keys []string
		for k := range manifest.Templates {
			keys = append(keys, k)
		}
		return "", "", fmt.Errorf("unknown template '%s'. Available: %s", templateKey, strings.Join(keys, ", "))
	}

	return meta.Subject, meta.File, nil
}

func buildMessage(fromEmail, toEmail, name, company, templateKey string) ([]byte, error) {
	if templateKey == "" {
		templateKey = "default"
	}

	data := EmailTemplateData{Name: name, Company: company}

	subjectTmpl, templateFile, err := resolveTemplatePath(templateKey)
	if err != nil {
		return nil, fmt.Errorf("resolve template: %w", err)
	}

	subject, err := renderSubject(subjectTmpl, data)
	if err != nil {
		return nil, fmt.Errorf("render subject: %w", err)
	}

	body, err := mail.RenderHTML("EMAIL_TEMPLATE_PATH", templateFile, data)
	if err != nil {
		return nil, fmt.Errorf("build body: %w", err)
	}

	resumePath := os.Getenv("RESUME_PATH")
	if resumePath == "" {
		resumeFilename := os.Getenv("RESUME_FILENAME")
		if resumeFilename == "" {
			resumeFilename = "ASUTOSH_DALEI_RESUME.pdf"
		}
		resumePath = filepath.Join("..", resumeFilename)
		if _, err := os.Stat(resumePath); os.IsNotExist(err) {
			resumePath = resumeFilename
		}
	}

	buf := new(bytes.Buffer)
	writer := multipart.NewWriter(buf)
	boundary := writer.Boundary()

	buf.WriteString(fmt.Sprintf("From: %s\r\n", fromEmail))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"text/html; charset=\"UTF-8\""},
	})
	if err != nil {
		return nil, err
	}
	htmlPart.Write([]byte(body))

	if _, err := os.Stat(resumePath); err == nil {
		fileData, err := os.ReadFile(resumePath)
		if err != nil {
			return nil, fmt.Errorf("read resume: %w", err)
		}

		encodedFile := make([]byte, base64.StdEncoding.EncodedLen(len(fileData)))
		base64.StdEncoding.Encode(encodedFile, fileData)

		var chunkedFile bytes.Buffer
		for i := 0; i < len(encodedFile); i += 76 {
			end := i + 76
			if end > len(encodedFile) {
				end = len(encodedFile)
			}
			chunkedFile.Write(encodedFile[i:end])
			chunkedFile.WriteString("\r\n")
		}

		attachmentPart, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Type":              []string{"application/pdf"},
			"Content-Transfer-Encoding": []string{"base64"},
			"Content-Disposition":       []string{fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(resumePath))},
		})
		if err != nil {
			return nil, err
		}
		attachmentPart.Write(chunkedFile.Bytes())
	} else {
		slog.Warn("Resume not found", "path", resumePath)
	}

	writer.Close()
	return buf.Bytes(), nil
}