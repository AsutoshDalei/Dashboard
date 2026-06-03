package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type EmailTemplateData struct {
	Name    string
	Company string
}

type SenderConfig struct {
	Label     string
	FromEmail string
	Mode      string
}

func GetConfig(senderKey string) (SenderConfig, error) {
	email := os.Getenv("EMAIL")
	universityEmail := os.Getenv("UNIVERSITY_EMAIL")
	if universityEmail == "" {
		universityEmail = email
	}
	personalEmail := os.Getenv("PERSONAL_EMAIL")
	if personalEmail == "" {
		personalEmail = email
	}

	modes := map[string]SenderConfig{
		"university": {Label: "University Gmail (API)", FromEmail: universityEmail, Mode: "gmail_api"},
		"personal":   {Label: "Personal Gmail (SMTP)", FromEmail: personalEmail, Mode: "smtp"},
	}

	config, ok := modes[senderKey]
	if !ok {
		var supported []string
		for k := range modes {
			supported = append(supported, k)
		}
		return SenderConfig{}, fmt.Errorf("unsupported sender_key '%s'. Supported values: %s", senderKey, strings.Join(supported, ", "))
	}

	if config.FromEmail == "" {
		return SenderConfig{}, fmt.Errorf("%s is missing from_email configuration", config.Label)
	}

	return config, nil
}

func draftEmailBody(name, company string) (string, error) {
	return renderHTMLRuntimeTemplate(
		"EMAIL_TEMPLATE_PATH",
		"templates/email_body.tmpl",
		EmailTemplateData{
			Name:    name,
			Company: company,
		},
	)
}

func buildEmailMessage(fromEmail, toEmail, name, company string) ([]byte, error) {
	subject := fmt.Sprintf("Interest in ML Engineer and Applied Scientist roles at %s", company)
	body, err := draftEmailBody(name, company)
	if err != nil {
		return nil, fmt.Errorf("failed to build email body: %w", err)
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

	// HTML part
	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"text/html; charset=\"UTF-8\""},
	})
	if err != nil {
		return nil, err
	}
	htmlPart.Write([]byte(body))

	// PDF Attachment
	if _, err := os.Stat(resumePath); err == nil {
		fileData, err := os.ReadFile(resumePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read resume: %w", err)
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

func sendViaSMTPBetter(msg []byte, fromEmail, toEmail string) error {
    password := os.Getenv("PASSWORD")
	if fromEmail == "" || password == "" {
		return fmt.Errorf("SMTP sender is not fully configured. Check PERSONAL_EMAIL/EMAIL and PASSWORD")
	}
    auth := smtp.PlainAuth("", fromEmail, password, "smtp.gmail.com")
    return smtp.SendMail("smtp.gmail.com:587", auth, fromEmail, []string{toEmail}, msg)
}

func sendViaGmailAPI(msg []byte) error {
	creds, err := getGmailCredentials()
	if err != nil {
		return err
	}

	ctx := context.Background()
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(creds))
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("unable to retrieve Gmail client: %w", err)
	}

	var message gmail.Message
	message.Raw = base64.URLEncoding.EncodeToString(msg)
	
	_, err = srv.Users.Messages.Send("me", &message).Do()
	return err
}

func SendEmail(toEmail, name, company, senderKey string) (string, error) {
	config, err := GetConfig(senderKey)
	if err != nil {
		return "", err
	}

	msg, err := buildEmailMessage(config.FromEmail, toEmail, name, company)
	if err != nil {
		return config.Label, fmt.Errorf("failed to build email message: %w", err)
	}

	if config.Mode == "gmail_api" {
		err = sendViaGmailAPI(msg)
	} else {
		err = sendViaSMTPBetter(msg, config.FromEmail, toEmail)
	}

	return config.Label, err
}
