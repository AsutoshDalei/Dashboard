package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CoverLetterTemplateData struct {
	Company string
}

var tectonicCompileSem = make(chan struct{}, 1)

func escapeLatexUserInput(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return "", fmt.Errorf("company name too long (max 200 characters)")
	}
	repl := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`{`, `\{`,
		`}`, `\}`,
		`$`, `\$`,
		`&`, `\&`,
		`#`, `\#`,
		`^`, `\^{}`,
		`_`, `\_`,
		`~`, `\~{}`,
		`%`, `\%`,
	)
	return repl.Replace(s), nil
}

func GenerateCoverLetterPDF(companyName string) ([]byte, error) {
	escaped, err := escapeLatexUserInput(companyName)
	if err != nil {
		return nil, err
	}

	tectonicCompileSem <- struct{}{}
	defer func() { <-tectonicCompileSem }()

	latex, err := renderRuntimeTemplate(
		"COVERLETTER_TEMPLATE_PATH",
		"templates/coverletter.tex.tmpl",
		CoverLetterTemplateData{Company: escaped},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build cover letter template: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "coverletter-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	texPath := filepath.Join(tempDir, "cover.tex")
	if err := os.WriteFile(texPath, []byte(latex), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write tex file: %w", err)
	}

	cmd := exec.Command("tectonic", texPath)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tectonic compilation failed: %w\nOutput: %s", err, string(output))
	}

	pdfPath := filepath.Join(tempDir, "cover.pdf")
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated PDF: %w", err)
	}

	return pdfData, nil
}
