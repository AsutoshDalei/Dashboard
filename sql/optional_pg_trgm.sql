-- Optional: faster tracker autocomplete when ENABLE_SUGGEST_TRGM=1 in the server env.
-- Run once against your tracker database (e.g. Supabase SQL editor).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS applications_org_lower_trgm_idx
  ON applications
  USING gin (lower(organization::text) gin_trgm_ops);
