Given the master resume and job description, provide surgical edits to optimize for ATS matching.

**Output ONLY valid JSON. No markdown, no code fences, no thinking process, no explanations.**

**Constraints:**
1. DO NOT increase word count — must stay 1 page
2. DO NOT invent skills or experience
3. ONLY rephrase existing text using JD keywords

**Resume structure for indices:**
WORK EXPERIENCE: "Nokia" (1 main item, 5 sub-items 0-4), "Maruti Suzuki" (2 main items — item 0 has 3 sub-items 0-2, item 1 has 2 sub-items 0-1)
PROJECTS: Agentic RAG (0), Spatiotemporal Modeling (1)
SKILLS: 6 categories — ML & AI, Experiment Tracking, Languages, Data Systems, Cloud, Databases

**Response format:**
{"experience_edits":[{"company":"Nokia","main_item_reorder":[0],"main_items":[{"rewrites":{"1":"text...","3":"text..."}}]}],"project_reorder":[1,0],"skills_swap":{"remove":["R"],"add":["A/B Testing"]},"changes_summary":"..."}

For main_item_reorder: indices in desired order (0-based). For rewrites: maps sub-item index to new text. Empty rewrites = no change.

**Escaping:** Bullet text must have LaTeX escaping: &→\&, %→\%, $→\$, #→\#, _→\_, {→\{, }→\}

## Master Resume (LaTeX)

```latex
{{RESUME_LATEX}}
```

## Job Description

{{JOB_DESCRIPTION}}

## Score: {{SCORE}}/5
## Keywords: {{KEYWORDS}}
## Recommendations: {{RECOMMENDATIONS}}
## Chat History: {{CHAT_HISTORY}}