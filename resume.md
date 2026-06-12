# Asutosh Dalei

**College Park, Maryland**  
asutoshdalei@gmail.com | LinkedIn | GitHub | Google Scholar | +1 240 432 1563

---

# WORK EXPERIENCE

## Machine Learning Engineering Intern
**Nokia – Sunnyvale, California**  
*June 2025 – June 2026*

### Knowledge Layer & RAG Architecture
- Designed and built the **Knowledge Layer**, a retrieval engine for querying complex technical documentation by unifying:
  - Vector Search (OpenSearch)
  - Relational metadata (PostgreSQL)
  - Graph traversal (Neo4j)

- Benchmarked 4+ SOTA embedding models against a validation corpus of 700+ technical manuals; selected **Embedding Gemma** for best semantic recall and built a custom sentence-aware chunking pipeline to optimize ingestion.

- Built a **Multimodal RAG** pipeline to index unstructured tables and figures using a joint vector space with **Jina Embeddings V4** and **OpenAI CLIP**, enabling text-to-visual retrieval.

- Added a re-ranking stage with **Qwen3 Reranker-8B (cross-encoder)**, which measurably improved context precision in retrieved chunks.

- Building an agentic retrieval framework with the **Google Agent Development Kit** to automate multi-step information gathering across the knowledge base.

**Stack:** Docker, Kafka, PostgreSQL, OpenSearch, Neo4j, MinIO, FastAPI, Python, GCP, AG2, vLLM

---

## Data Scientist
**Maruti Suzuki (R&D Division) – Bengaluru, India**  
*January 2022 – August 2024*

### Battery Health Prediction (Predictive Maintenance)
- Built an LSTM-based time-series classifier on telematics data to predict lead-acid battery failure in connected vehicles. Trained on imbalanced sensor data using focal cross-entropy loss.

- Designed a 14-day sliding window aggregation layer over raw predictions to suppress sensor noise and reduce false positives before alerts reached the field.

- Deployed across 100K+ connected vehicles with **98% field-validated accuracy**, reducing warranty claims and roadside failures.

- Work resulted in a published patent (**No. 202411040338**).

### MLOps & Training Cost Reduction
- Audited a legacy training pipeline consuming approximately **$4,000/month** in GPU compute.

- Profiled bottlenecks and migrated Random Forest workloads to optimized multi-core CPU execution, eliminating unnecessary GPU spend.

- Integrated **Optuna** for automated hyperparameter search, reducing experiment runtime and cloud compute costs.

---

# EDUCATION

## Master of Science, Data Science
**University of Maryland, College Park**  
*August 2024 – May 2026*  
**GPA:** 3.967 / 4.0

**Relevant Coursework**
- Advanced Machine Learning
- Natural Language Processing
- Probability & Statistics
- Data Representation & Modelling
- Big Data Systems
- Cloud Computing

---

## Bachelor of Technology, Electrical and Electronics Engineering
**Vellore Institute of Technology, Vellore**  
*2018 – 2022*  
**GPA:** 9.1 / 10

---

# RESEARCH & PATENTS

## Patent – Automobile Battery Health Prediction System
**Patent No. 202411040338**

- Primary ML contributor on a patented system for predicting remaining useful life of lead-acid batteries using telematics sensor data and LSTM-based deep learning.

- Deployed in production across Maruti Suzuki's connected vehicle fleet.

---

## Molecular Signatures and ML-driven Stress Biomarkers for Rainbow Trout Aquaculture
**Nature Scientific Reports, 2025**

- Built predictive models on genomic data to identify stress biomarkers in rainbow trout.

- Contributed model design, feature selection across high-dimensional gene expression datasets, and analysis of markers tied to environmental stress response.

---

# TECHNICAL SKILLS

### ML & AI
- PyTorch
- TensorFlow
- Hugging Face
- Transformers
- LLM Fine-Tuning (SFT, RLHF, LoRA, PEFT)
- RAG
- Agentic AI
- LangChain
- LLM Evaluation
- Structured Output Enforcement
- Inference Optimization
- Model Serving (vLLM, SGLang)

### Experiment Tracking & MLOps
- MLflow
- Optuna
- Weights & Biases
- Jenkins CI/CD
- GitHub Actions

### Languages
- Python
- Go
- R
- SQL
- MATLAB
- C
- C++

### Distributed & Data Systems
- Kafka
- FastAPI
- Docker
- Kubernetes

### Cloud & Infrastructure
- GCP
- Azure
- Linux
- Git

### Databases
- PostgreSQL
- MongoDB
- MySQL
- OpenSearch
- Neo4j

---

# PROJECTS

## Agentic RAG over Enterprise Knowledge Graphs

- Designed an enterprise-ready modular agentic framework (AG2) for natural-language querying over enterprise knowledge graphs spanning Ab Initio workflows.

- Deployed on Kubernetes and served via Vertex AI.

- Integrated ZeRank2 re-ranking to improve retrieval precision.

---

## Spatiotemporal Modeling of Telematics Data Gaps

- Traced recurring multi-hour data dropouts (~5% of telematics records) to regional network dead zones.

- Built an ML pipeline using:
  - Random Forests for anomaly classification
  - GRU-based spatiotemporal sequence modeling for geographic cluster identification

- Improved data collection completeness by **15%** across the connected vehicle fleet.
