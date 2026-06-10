package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type GmailProvider struct {
	tokenPath string
}

func NewGmailProvider(tokenPath string) *GmailProvider {
	return &GmailProvider{tokenPath: tokenPath}
}

func (p *GmailProvider) Send(fromEmail, toEmail, name, company string) (string, error) {
	msg, err := buildMessage(fromEmail, toEmail, name, company)
	if err != nil {
		return "", fmt.Errorf("build message: %w", err)
	}

	creds, err := getGmailCredentials(p.tokenPath)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(creds))
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

func getGmailCredentials(tokenPath string) (*oauth2.Token, error) {
	targetPath := tokenPath
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		targetPath = "token.json"
	}

	credsData, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}
	_ = os.Chmod(targetPath, 0o600)

	var pt struct {
		Token        string   `json:"token"`
		RefreshToken string   `json:"refresh_token"`
		TokenURI     string   `json:"token_uri"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		Expiry       string   `json:"expiry"`
	}

	if err := json.Unmarshal(credsData, &pt); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	expiry, err := time.Parse(time.RFC3339Nano, pt.Expiry)
	if err != nil {
		expiry, err = time.Parse(time.RFC3339, pt.Expiry)
		if err != nil {
			return nil, fmt.Errorf("parse expiry: %w", err)
		}
	}

	token := &oauth2.Token{
		AccessToken:  pt.Token,
		TokenType:    "Bearer",
		RefreshToken: pt.RefreshToken,
		Expiry:       expiry,
	}

	conf := &oauth2.Config{
		ClientID:     pt.ClientID,
		ClientSecret: pt.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: pt.TokenURI,
		},
		Scopes: pt.Scopes,
	}

	tokenSource := conf.TokenSource(context.Background(), token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	if newToken.AccessToken != pt.Token {
		pt.Token = newToken.AccessToken
		pt.RefreshToken = newToken.RefreshToken
		pt.Expiry = newToken.Expiry.Format(time.RFC3339Nano)
		updated, _ := json.Marshal(pt)
		_ = os.WriteFile(targetPath, updated, 0o600)
	}

	return newToken, nil
}