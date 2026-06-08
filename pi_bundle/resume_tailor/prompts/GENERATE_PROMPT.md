Given the master resume LaTeX, job description, analysis score, keywords, and chat history, modify the resume LaTeX to better align with the JD.

**CRITICAL:** Output ONLY valid JSON. No markdown, no code fences, no thinking process, no explanations.

**Constraints:**
1. DO NOT increase word count — must stay 1 page
2. DO NOT add new bullet points or sections
3. DO NOT invent skills
4. ONLY rephrase existing bullets using JD keywords
5. Technical Skills: may swap irrelevant skills for relevant ones
6. NEVER use clichés: passionate about, results-oriented, proven track record, leveraged, spearheaded, facilitated, synergies, robust, seamless, cutting-edge, innovative

**Keyword injection examples:**
- JD says "RAG pipelines" and resume says "LLM workflows with retrieval" → "RAG pipeline design and LLM orchestration workflows"
- JD says "MLOps" and resume says "observability, evals" → "MLOps and observability: evals, error handling, cost monitoring"

Return: {"modified_latex":"...","changes_summary":"...","keywords_injected":[],"skills_removed":[],"skills_added":[]}

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