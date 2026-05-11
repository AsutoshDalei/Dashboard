package main

import (
	"bytes"
	"embed"
	"fmt"
	htmpl "html/template"
	"os"
	"path/filepath"
	"strings"
	ttmpl "text/template"
)

//go:embed templates/email_body.tmpl templates/coverletter.tex.tmpl
var runtimeTmplFS embed.FS

func resolveRuntimeTemplateBytes(envVar, embedPath string) ([]byte, error) {
	if p := strings.TrimSpace(os.Getenv(envVar)); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s=%q: %w", envVar, p, err)
		}
		return b, nil
	}
	if b, err := runtimeTmplFS.ReadFile(embedPath); err == nil {
		return b, nil
	}
	candidates := []string{embedPath, filepath.Join("..", embedPath)}
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("template not found (embed %s or disk paths %v)", embedPath, candidates)
}

// renderRuntimeTemplate renders LaTeX-oriented content with text/template (no HTML escaping).
func renderRuntimeTemplate(envVar, embedPath string, data any) (string, error) {
	b, err := resolveRuntimeTemplateBytes(envVar, embedPath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(embedPath)
	tmpl, err := ttmpl.New(name).Parse(string(b))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", embedPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", embedPath, err)
	}
	return buf.String(), nil
}

// renderHTMLRuntimeTemplate renders HTML email bodies with html/template (auto-escaping).
func renderHTMLRuntimeTemplate(envVar, embedPath string, data any) (string, error) {
	b, err := resolveRuntimeTemplateBytes(envVar, embedPath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(embedPath)
	tmpl, err := htmpl.New(name).Parse(string(b))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML template %s: %w", embedPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render HTML template %s: %w", embedPath, err)
	}
	return buf.String(), nil
}
