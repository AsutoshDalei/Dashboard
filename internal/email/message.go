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
)

type EmailTemplateData struct {
	Name    string
	Company string
	Role    string
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

func renderHTMLTemplate(filePath string, data EmailTemplateData) (string, error) {
	candidates := []string{filePath, filepath.Join("..", filePath)}
	var b []byte
	var err error
	for _, c := range candidates {
		b, err = os.ReadFile(c)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", filePath, err)
	}

	name := filepath.Base(filePath)
	tmpl, err := template.New(name).Parse(string(b))
	if err != nil {
		return "", fmt.Errorf("parse html template %s: %w", filePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render html template %s: %w", filePath, err)
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

func buildMessage(fromEmail, toEmail, name, company, templateKey, role string, attachResume bool) ([]byte, error) {
	if templateKey == "" {
		templateKey = "referral"
	}

	data := EmailTemplateData{Name: name, Company: company, Role: role}

	subjectTmpl, templateFile, err := resolveTemplatePath(templateKey)
	if err != nil {
		return nil, fmt.Errorf("resolve template: %w", err)
	}

	subject, err := renderSubject(subjectTmpl, data)
	if err != nil {
		return nil, fmt.Errorf("render subject: %w", err)
	}

	body, err := renderHTMLTemplate(templateFile, data)
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

	if attachResume {
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
	}

	writer.Close()
	return buf.Bytes(), nil
}