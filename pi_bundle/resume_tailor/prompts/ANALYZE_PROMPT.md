## Pipeline — Análisis de Match

### Paso 0 — Detección de Arquetipo

Clasifica la oferta en uno de los 6 arquetipos. Si es híbrido, indica los 2 más cercanos.

**Los 6 arquetipos:**

| Arquetipo | Ejes temáticos | Qué compran |
|-----------|----------------|-------------|
| **AI Platform / LLMOps Engineer** | Evaluation, observability, reliability, pipelines | Alguien que ponga AI en producción con métricas |
| **Agentic Workflows / Automation** | HITL, tooling, orchestration, multi-agent | Alguien que construya sistemas de agentes fiables |
| **Technical AI Product Manager** | GenAI/Agents, PRDs, discovery, delivery | Alguien que traduzca negocio → producto AI |
| **AI Solutions Architect** | Hyperautomation, enterprise, integrations | Alguien que diseñe arquitecturas AI end-to-end |
| **AI Forward Deployed Engineer** | Client-facing, fast delivery, prototyping | Alguien que entregue soluciones AI a clientes rápido |
| **AI Transformation Lead** | Change management, adoption, org enablement | Alguien que lidere el cambio AI en una organización |

### Paso 1 — Extraer Keywords

Extrae 15-20 keywords del JD para ATS. Prioriza:
1. Required technical skills (languages, frameworks, tools, platforms)
2. Domain-specific terminology
3. Role-specific responsibilities
4. Soft skills mentioned repeatedly

### Paso 2 — Evaluación de Match

Evalúa el match entre el master resume y el JD. Considera:

**Match con CV:** Skills, experience, proof points alignment. Cada requisito del JD debe mapearse a líneas exactas del CV. Identifica gaps con estrategia de mitigación para cada uno:
1. ¿Es hard blocker o nice-to-have?
2. Can the candidate demonstrate experiencia adyacente?
3. ¿Hay un proyecto que cubra este gap?
4. Plan de mitigación concreto

**North Star alignment:** How well the role fits the candidate's target archetypes.

**Comp:** Salary vs market (5=top quartile, 1=well below).

**Cultural signals:** Company culture, growth, stability, remote policy.

**Red flags:** Blockers, warnings (negative adjustments).

### Paso 3 — Score Global

| Dimensión | Score |
|-----------|-------|
| Match con CV | X/5 |
| Alineación North Star | X/5 |
| Comp | X/5 |
| Señales culturales | X/5 |
| Red flags | -X (si hay) |
| **Global** | **X/5** |

### Paso 4 — Plan de Personalización

Top cambios recomendados al resume para alinearlo con el JD.

## Master Resume (LaTeX)

```latex
{{RESUME_LATEX}}
```

## Job Description

```
{{JOB_DESCRIPTION}}
```

Return ONLY valid JSON with fields: score (float), keywords (array of strings), analysis (string), recommendations (string), archetype (string).