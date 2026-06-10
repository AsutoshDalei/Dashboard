package mail

import (
	"bytes"
	"fmt"
	htmpl "html/template"
	"os"
	"path/filepath"
	"strings"
	ttmpl "text/template"
)

func ResolveTemplateBytes(envVar, defaultPath string) ([]byte, error) {
	if p := strings.TrimSpace(os.Getenv(envVar)); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s=%q: %w", envVar, p, err)
		}
		return b, nil
	}
	candidates := []string{defaultPath, filepath.Join("..", defaultPath)}
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", defaultPath)
}

func RenderText(envVar, defaultPath string, data any) (string, error) {
	b, err := ResolveTemplateBytes(envVar, defaultPath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(defaultPath)
	tmpl, err := ttmpl.New(name).Parse(string(b))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", defaultPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template %s: %w", defaultPath, err)
	}
	return buf.String(), nil
}

func RenderHTML(envVar, defaultPath string, data any) (string, error) {
	b, err := ResolveTemplateBytes(envVar, defaultPath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(defaultPath)
	tmpl, err := htmpl.New(name).Parse(string(b))
	if err != nil {
		return "", fmt.Errorf("parse html template %s: %w", defaultPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render html template %s: %w", defaultPath, err)
	}
	return buf.String(), nil
}