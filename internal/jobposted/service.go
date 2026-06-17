package jobposted

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const externalAPIURL = "https://mcp.whenthisjobwasposted.com/api/v1/check"

type Service struct {
	client *http.Client
}

func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (s *Service) CheckPostedDate(ctx context.Context, jobURL string) (*PostedDateData, error) {
	reqURL := externalAPIURL + "?url=" + url.QueryEscape(jobURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("external api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external api returned status %d: %s", resp.StatusCode, string(body))
	}

	var data PostedDateData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &data, nil
}
