package components

import (
	"fmt"
	"html/template"
	"strings"
)

type ButtonProps struct {
	Text    string
	URL     string
	Variant string // primary, light, danger
	Full    bool
	Class   string
}

func Button(p ButtonProps) template.HTML {
	classes := []string{"btn"}
	switch p.Variant {
	case "primary":
		classes = append(classes, "btn-primary")
	case "light":
		classes = append(classes, "btn-light")
	case "danger":
		classes = append(classes, "bg-error text-white hover:brightness-110")
	default:
		classes = append(classes, "btn-light")
	}
	if p.Full {
		classes = append(classes, "btn-full")
	}
	if p.Class != "" {
		classes = append(classes, p.Class)
	}

	classAttr := strings.Join(classes, " ")

	if p.URL != "" {
		return template.HTML(fmt.Sprintf(`<a href="%s" class="%s"><span class="btn-text">%s</span></a>`, p.URL, classAttr, p.Text))
	}
	return template.HTML(fmt.Sprintf(`<button type="button" class="%s"><span class="btn-text">%s</span></button>`, classAttr, p.Text))
}

type CardProps struct {
	Icon        string
	Title       string
	Description string
	Link        string
	LinkText    string
	Class       string
}

func Card(p CardProps) template.HTML {
	class := "card"
	if p.Class != "" {
		class += " " + p.Class
	}

	var linkHTML string
	if p.Link != "" {
		linkText := p.LinkText
		if linkText == "" {
			linkText = "Open Tool"
		}
		linkHTML = fmt.Sprintf(`<a href="%s" class="btn btn-light"><span class="btn-text">%s</span><span class="btn-icon">→</span></a>`, p.Link, linkText)
	}

	return template.HTML(fmt.Sprintf(`<div class="%s">
		<div class="card-icon">%s</div>
		<h3 class="card-title">%s</h3>
		<p class="card-description">%s</p>
		%s
	</div>`, class, p.Icon, p.Title, p.Description, linkHTML))
}

type InputProps struct {
	Label       string
	Name        string
	Type        string
	Placeholder string
	Required    bool
	Value       string
	Class       string
}

func Input(p InputProps) template.HTML {
	inputType := p.Type
	if inputType == "" {
		inputType = "text"
	}

	required := ""
	if p.Required {
		required = " required"
	}

	value := ""
	if p.Value != "" {
		value = fmt.Sprintf(` value="%s"`, p.Value)
	}

	class := "form-input"
	if p.Class != "" {
		class += " " + p.Class
	}

	return template.HTML(fmt.Sprintf(`<div class="form-group">
		<label class="form-label" for="%s">%s</label>
		<input type="%s" id="%s" name="%s" class="%s" placeholder="%s"%s%s>
	</div>`, p.Name, p.Label, inputType, p.Name, p.Name, class, p.Placeholder, required, value))
}

type BadgeProps struct {
	Text  string
	Color string
}

func Badge(p BadgeProps) template.HTML {
	color := p.Color
	if color == "" {
		color = "bg-surface-high text-on-surface-variant"
	}
	return template.HTML(fmt.Sprintf(`<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium %s">%s</span>`, color, p.Text))
}