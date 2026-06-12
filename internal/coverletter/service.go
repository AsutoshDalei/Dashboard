package coverletter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pi_dashboard/pkg/mail"
)

type CoverLetterTemplateData struct {
	Company string
}

var tectonicCompileSem = make(chan struct{}, 1)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) GeneratePDF(companyName string) ([]byte, error) {
	escaped, err := escapeLatexUserInput(companyName)
	if err != nil {
		return nil, err
	}

	tectonicCompileSem <- struct{}{}
	defer func() { <-tectonicCompileSem }()

	latex, err := mail.RenderText("COVERLETTER_TEMPLATE_PATH", "templates/coverletter.tex.tmpl", CoverLetterTemplateData{Company: escaped})
	if err != nil {
		return nil, fmt.Errorf("build cover letter: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "coverletter-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	texPath := filepath.Join(tempDir, "cover.tex")
	if err := os.WriteFile(texPath, []byte(latex), 0o644); err != nil {
		return nil, fmt.Errorf("write tex: %w", err)
	}

	cmd := exec.Command("tectonic", texPath)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tectonic failed: %w\nOutput: %s", err, string(output))
	}

	pdfData, err := os.ReadFile(filepath.Join(tempDir, "cover.pdf"))
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}

	return pdfData, nil
}

func escapeLatexUserInput(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return "", fmt.Errorf("company name too long (max 200)")
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