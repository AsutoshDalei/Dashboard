# Mode: latex — LaTeX Resume Tailoring

Export a tailored, ATS-optimized resume as a `.tex` document.

## Pipeline

1. Read the master resume LaTeX as source of truth
2. Read the JD (provided below)
3. Extract 15-20 keywords from the JD
4. Detect JD language → resume language (EN default)
5. Detect role archetype → adapt framing
6. Rewrite Professional Summary injecting JD keywords (same rules as below — NEVER invent skills)
7. Select top 3-4 most relevant projects for the offer
8. Reorder experience bullets by JD relevance
9. Inject keywords naturally into existing achievements
10. Generate the modified `.tex` document

## LaTeX Content Generation Rules

The master resume uses a custom LaTeX format (not sb2nov/resume). Work with the existing structure:
- Section headers use `\section*{SECTION NAME}`
- Experience entries use `\textbf{Role} \hfill \textit{Dates}\\` followed by `Company -- Location`
- Bullet points use `\begin{itemize} \item ... \end{itemize}`
- Nested bullets use nested `itemize` environments
- Skills use `\textbf{Category}{: items}` inside `\begin{itemize}`

### What you CAN change:
1. **Professional Summary**: The resume does not have a separate summary section. The header with name and contact info stays fixed. Focus on rephrasing experience bullets.
2. **Experience bullets**: Reorder by JD relevance. Rephrase existing bullets to inject JD keywords naturally. NEVER add new bullets or invent experience.
3. **Technical Skills section**: You may remove irrelevant skills and replace with relevant ones from the JD. This is the ONLY section where content can be swapped.
4. **Projects**: Reorder — put the most relevant project first.

### What you CANNOT change:
1. DO NOT add new bullet points or sections
2. DO NOT increase the total word count — the resume must remain exactly 1 page
3. DO NOT invent skills or experience the candidate doesn't have
4. DO NOT modify the header (name, contact info, links)
5. DO NOT modify Education or Research & Patents sections
6. DO NOT modify company names, role titles, dates, or locations

## ATS Rules

- Single-column layout (enforced by template)
- Standard section headers: WORK EXPERIENCE, EDUCATION, TECHNICAL SKILLS, PROJECTS, RESEARCH & PATENTS
- UTF-8, machine-readable via `\pdfgentounicode=1`
- Keywords distributed: first bullet of each role, skills section
- No images, no graphics, no color in body text

## Keyword Injection Strategy

Same ethical rules:
- NEVER add skills the candidate doesn't have
- Only reformulate existing experience using JD vocabulary
- Examples:
  - JD says "RAG pipelines" → reword "LLM workflows with retrieval" to "RAG pipeline design"
  - JD says "MLOps" → reword "observability, evals" to "MLOps and observability"

## Context

**Master Resume (LaTeX):**
```latex
{{RESUME_LATEX}}
```

**Job Description:**
```
{{JOB_DESCRIPTION}}
```

**Analysis Score:** {{SCORE}}/5
**Keywords from JD:** {{KEYWORDS}}
**Recommendations:** {{RECOMMENDATIONS}}
**Chat History (user feedback):** {{CHAT_HISTORY}}

Return ONLY valid JSON:
```json
{
  "modified_latex": "The ENTIRE modified LaTeX document as a single string with escaped newlines",
  "changes_summary": "Brief summary of what was changed",
  "keywords_injected": ["keyword1", "keyword2"],
  "skills_removed": ["skill1"],
  "skills_added": ["skill2"]
}
```