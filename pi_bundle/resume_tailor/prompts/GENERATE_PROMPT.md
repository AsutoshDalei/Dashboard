Given the master resume LaTeX and job description, provide surgical edits to optimize the resume for ATS matching.

**Output ONLY valid JSON. No markdown, no code fences, no thinking process, no explanations.**

**Constraints:**
1. DO NOT increase word count — must stay 1 page
2. DO NOT invent skills or experience
3. ONLY rephrase existing text using JD keywords
4. Never use clichés: passionate about, results-oriented, proven track record, leveraged, spearheaded, facilitated, synergies, robust, seamless, cutting-edge, innovative

**Keyword injection strategy:**
- Never add skills the candidate doesn't have
- Only reformulate existing experience using JD vocabulary
- Example: JD says "RAG pipelines" and resume says "LLM workflows with retrieval" → "RAG pipeline design and LLM orchestration workflows"

**Resume structure (for edit indices):**

WORK EXPERIENCE section has 2 companies:
- Company "Nokia": 1 main item (Knowledge Layer & RAG Architecture) with 5 sub-items (indices 0-4)
- Company "Maruti Suzuki": 2 main items — Battery Health Prediction (index 0, 3 sub-items 0-2), MLOps (index 1, 2 sub-items 0-1)

PROJECTS section has 2 projects: Agentic RAG over Enterprise Knowledge Graphs (index 0), Spatiotemporal Modeling of Telematics Data Gaps (index 1)

TECHNICAL SKILLS: 6 categories — ML & AI, Experiment Tracking & MLOps, Languages, Distributed & Data Systems, Cloud & Infrastructure, Databases

**Edits you can make:**
- `experience_edits`: For each company, reorder main items and/or rewrite sub-item text
- `project_reorder`: Reorder the 2 projects by relevance to the JD
- `skills_swap`: Remove irrelevant skills, add relevant ones from the JD

**Response format:**
```json
{
  "experience_edits": [
    {
      "company": "Nokia",
      "main_item_reorder": [0],
      "main_items": [
        {"rewrites": {"1": "new bullet text...", "3": "new bullet text..."}}
      ]
    },
    {
      "company": "Maruti Suzuki",
      "main_item_reorder": [1, 0],
      "main_items": [
        {"rewrites": {"0": "new text for item 1's sub-bullet 0..."}},
        {"rewrites": {}}
      ]
    }
  ],
  "project_reorder": [1, 0],
  "skills_swap": {"remove": ["R", "MySQL"], "add": ["A/B Testing", "TensorFlow"]},
  "changes_summary": "Summary of edits made"
}
```

For `main_item_reorder`, list the indices of main items in desired order (0-based). For `main_items`, each entry corresponds to a main item (in reordered order). `rewrites` maps sub-item index to new text — only include bullets that need rewriting. Empty rewrites means no change.

**IMPORTANT: The new bullet text must contain LaTeX escaping for special characters: `&` → `\&`, `%` → `\%`, `$` → `\$`, `#` → `\#`, `_` → `\_`, `{` → `\{`, `}` → `\}`

## Master Resume (LaTeX)

```latex
{{RESUME_LATEX}}
```

## Job Description

{{JOB_DESCRIPTION}}

## Analysis Score: {{SCORE}}/5
## Keywords: {{KEYWORDS}}
## Recommendations: {{RECOMMENDATIONS}}
## Chat History: {{CHAT_HISTORY}}