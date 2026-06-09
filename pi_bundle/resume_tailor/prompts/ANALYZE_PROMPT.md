## Pipeline — ATS Match Analysis

Analyze the master resume against the job description. Extract 15-20 keywords from the JD, score the match on 1-5, and provide analysis, recommendations, and archetype. Be brutally honest — if the resume doesn't match, say so explicitly.

### Evaluation Dimensions

**Match con CV:** Skills, experience, proof points alignment. Map each JD requirement to exact lines from the resume. Identify gaps with mitigation: is it a hard blocker or nice-to-have? Can adjacent experience cover it?

**North Star alignment:** How well the role fits the candidate's target archetypes.

**Comp:** Salary vs market.

**Cultural signals:** Company culture, growth, stability, remote policy.

**Red flags:** Blockers, warnings (negative adjustments).

### Score Table

| Dimension | Score |
|-----------|-------|
| Match con CV | X/5 |
| North Star alignment | X/5 |
| Comp | X/5 |
| Cultural signals | X/5 |
| Red flags | -X (if any) |
| **Global** | **X/5** |

### Archetype Detection

Classify into: AI Platform/LLMOps, Agentic/Automation, Technical AI PM, AI Solutions Architect, AI Forward Deployed, AI Transformation (or hybrid of 2).

## Master Resume (LaTeX)

```latex
{{RESUME_LATEX}}
```

## Job Description

```
{{JOB_DESCRIPTION}}
```

Return ONLY valid JSON: {"score":N,"keywords":[],"analysis":"","recommendations":"","archetype":""}
