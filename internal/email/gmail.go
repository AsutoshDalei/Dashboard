package email

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type GmailProvider struct {
	accessToken  string
	refreshToken string
	clientID     string
	clientSecret string
	tokenURI     string
	expiry       string
}

func NewGmailProvider(accessToken, refreshToken, clientID, clientSecret, tokenURI, expiry string) *GmailProvider {
	return &GmailProvider{
		accessToken:  accessToken,
		refreshToken: refreshToken,
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURI:     tokenURI,
		expiry:       expiry,
	}
}

func (p *GmailProvider) Send(fromEmail, toEmail, name, company, templateKey, role string, attachResume bool) (string, error) {
	msg, err := buildMessage(fromEmail, toEmail, name, company, templateKey, role, attachResume)
	if err != nil {
		return "", fmt.Errorf("build message: %w", err)
	}

	creds, err := p.getToken()
	if err != nil {
		return "", err
	}

	conf := &oauth2.Config{
		ClientID:     p.clientID,
		ClientSecret: p.clientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: p.tokenURI,
		},
	}

	ctx := context.Background()
	tokenSource := conf.TokenSource(ctx, creds)
	client := oauth2.NewClient(ctx, tokenSource)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return "", fmt.Errorf("gmail client: %w", err)
	}

	var message gmail.Message
	message.Raw = base64.URLEncoding.EncodeToString(msg)

	_, err = srv.Users.Messages.Send("me", &message).Do()
	if err != nil {
		return "", fmt.Errorf("gmail send: %w", err)
	}

	return "Gmail API", nil
}

func (p *GmailProvider) getToken() (*oauth2.Token, error) {
	if p.accessToken == "" || p.refreshToken == "" {
		return nil, fmt.Errorf("Gmail OAuth2 credentials not configured. Set GMAIL_ACCESS_TOKEN and GMAIL_REFRESH_TOKEN in .env")
	}

	expiry, err := time.Parse(time.RFC3339Nano, p.expiry)
	if err != nil {
		expiry, err = time.Parse(time.RFC3339, p.expiry)
		if err != nil {
			return nil, fmt.Errorf("parse expiry: %w", err)
		}
	}

	token := &oauth2.Token{
		AccessToken:  p.accessToken,
		TokenType:    "Bearer",
		RefreshToken: p.refreshToken,
		Expiry:       expiry,
	}

	return token, nil
}
