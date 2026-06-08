## Pipeline — Análisis de Match

Given the master resume text and job description below, evaluate the match.

### What to evaluate:
1. **Score (1-5):** Match con CV, North Star alignment, Comp, Cultural signals, Red flags
2. **Keywords:** Extract 15-20 key terms from the JD for ATS
3. **Analysis:** 2-3 sentence match quality assessment
4. **Recommendations:** 2-3 sentence actionable tailoring suggestions
5. **Archetype:** Classify into: AI Platform/LLMOps, Agentic/Automation, Technical AI PM, AI Solutions Architect, AI Forward Deployed, AI Transformation

**Score guide:** 4.5+ Strong, 4.0-4.4 Good, 3.5-3.9 Decent, Below 3.5 Weak.

Return ONLY valid JSON: {"score":N,"keywords":[],"analysis":"","recommendations":"","archetype":""}

## Master Resume

{{RESUME_TEXT}}

## Job Description

{{JOB_DESCRIPTION}}