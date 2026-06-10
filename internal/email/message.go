package email

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"

	"pi_dashboard/pkg/mail"
)

type EmailTemplateData struct {
	Name    string
	Company string
}

func buildMessage(fromEmail, toEmail, name, company string) ([]byte, error) {
	subject := fmt.Sprintf("Interest in ML Engineer and Applied Scientist roles at %s", company)
	body, err := mail.RenderHTML("EMAIL_TEMPLATE_PATH", "templates/email_body.tmpl", EmailTemplateData{Name: name, Company: company})
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