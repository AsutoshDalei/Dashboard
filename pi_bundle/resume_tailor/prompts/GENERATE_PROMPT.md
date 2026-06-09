Given the master resume and job description, optimize the Technical Skills section to align with the JD.

The resume has 6 categories. For each category, you can:
1. **Remove irrelevant skills** via `skills_to_remove`
2. **Add relevant JD skills** the candidate clearly has via `skills_to_add` 
3. **Completely rewrite a category line** via `category_rewrites` — replace the entire comma-separated list after the category name to better match JD terminology while keeping the candidate's real skills. Only rewrite categories where the current list needs significant rework.

Rules:
- Never add skills the candidate doesn't have evidence for (check their experience bullets)
- Skills to add must be explicitly mentioned in the JD AND demonstrably present in the candidate's experience
- category_rewrites: the value is the full list text (e.g., "PyTorch, TensorFlow, RAG, Agentic AI, A/B Testing"). Include ONLY the skills the candidate actually has. You can reorder, regroup, and rephrase to match JD emphasis.

## Master Resume (LaTeX)

```latex
{{RESUME_LATEX}}
```

## Job Description

{{JOB_DESCRIPTION}}

## Analysis Score: {{SCORE}}/5
## Keywords: {{KEYWORDS}}
## Recommendations: {{RECOMMENDATIONS}}