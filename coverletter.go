package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type CoverLetterTemplateData struct {
	Company string
}

func GenerateCoverLetterPDF(companyName string) ([]byte, error) {
	latex, err := renderRuntimeTemplate(
		"COVERLETTER_TEMPLATE_PATH",
		filepath.Join("templates", "coverletter.tex.tmpl"),
		CoverLetterTemplateData{Company: companyName},
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
	if err := os.WriteFile(texPath, []byte(latex), 0644); err != nil {
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
