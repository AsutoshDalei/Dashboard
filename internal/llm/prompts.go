package llm

import (
	"encoding/json"
	"os"
	"strings"
)

type Prompts struct {
	ChatSystem        string `json:"chat_system"`
	ResumeAnalyze     string `json:"resume_analyze"`
	ResumeGenerate    string `json:"resume_generate"`
	ResumeChat        string `json:"resume_chat"`
	EmailDraft        string `json:"email_draft"`
	CoverLetterDraft  string `json:"coverletter_draft"`
	JobMatch          string `json:"job_match"`
	SQLAssistant      string `json:"sql_assistant"`
}

func LoadPrompts(path string) (*Prompts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var p Prompts
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}

	return &p, nil
}

func (p *Prompts) Get(key string) string {
	switch key {
	case "chat_system":
		return p.ChatSystem
	case "resume_analyze":
		return p.ResumeAnalyze
	case "resume_generate":
		return p.ResumeGenerate
	case "resume_chat":
		return p.ResumeChat
	case "email_draft":
		return p.EmailDraft
	case "coverletter_draft":
		return p.CoverLetterDraft
	case "job_match":
		return p.JobMatch
	case "sql_assistant":
		return p.SQLAssistant
	default:
		return ""
	}
}

func (p *Prompts) Format(key string, replacements map[string]string) string {
	template := p.Get(key)
	for k, v := range replacements {
		template = strings.ReplaceAll(template, "{"+k+"}", v)
	}
	return template
}