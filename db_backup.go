package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultDumpDir        = "db_dumps"
	defaultBackupInterval = 24 * time.Hour
	defaultMaxRetries     = 2
	defaultRowTolerance   = 0
	defaultBackupSchemas  = "public"
	pgDumpTimeout         = 30 * time.Minute
	verifyPostDumpSleep   = 5 * time.Second
	schemaDumpFilename    = "schema.sql"
	dataDumpFilename      = "data.sql"
)

type backupConfig struct {
	dumpDir      string
	interval     time.Duration
	maxRetries   int
	rowTolerance int
	schemas      []string
	allSchemas   bool
}

func loadBackupConfig() backupConfig {
	cfg := backupConfig{
		dumpDir:      defaultDumpDir,
		interval:     defaultBackupInterval,
		maxRetries:   defaultMaxRetries,
		rowTolerance: defaultRowTolerance,
		schemas:      []string{defaultBackupSchemas},
	}

	if v := strings.TrimSpace(os.Getenv("DB_DUMP_DIR")); v != "" {
		cfg.dumpDir = v
	}
	if v := strings.TrimSpace(os.Getenv("DB_BACKUP_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.interval = d
		} else {
			log.Printf("backup: invalid DB_BACKUP_INTERVAL=%q, using %s", v, cfg.interval)
		}
	}
	if v := strings.TrimSpace(os.Getenv("DB_BACKUP_MAX_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.maxRetries = n
		} else {
			log.Printf("backup: invalid DB_BACKUP_MAX_RETRIES=%q, using %d", v, cfg.maxRetries)
		}
	}
	if v := strings.TrimSpace(os.Getenv("DB_BACKUP_ROW_TOLERANCE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.rowTolerance = n
		} else {
			log.Printf("backup: invalid DB_BACKUP_ROW_TOLERANCE=%q, using %d", v, cfg.rowTolerance)
		}
	}
	if v := strings.TrimSpace(os.Getenv("DB_BACKUP_SCHEMAS")); v != "" {
		if v == "*" {
			cfg.schemas = nil
			cfg.allSchemas = true
		} else {
			parts := strings.Split(v, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			if len(out) > 0 {
				cfg.schemas = out
			}
		}
	}
	return cfg
}

func backupDisabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("DISABLE_DB_BACKUP"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func schemasLabel(cfg backupConfig) string {
	if cfg.allSchemas {
		return "*"
	}
	return strings.Join(cfg.schemas, ",")
}

// startDatabaseBackups runs the schema dump synchronously, then starts a
// detached goroutine that performs the data dump immediately and again on a
// fixed interval. Errors are logged, never fatal: a backup hiccup must not
// take down the HTTP server.
func startDatabaseBackups(ctx context.Context, databaseURL string) {
	if backupDisabled() {
		log.Printf("backup: DISABLE_DB_BACKUP set; skipping automated dumps")
		return
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		log.Printf("backup: pg_dump not found in PATH; automated dumps disabled (%v)", err)
		return
	}

	cfg := loadBackupConfig()
	if err := os.MkdirAll(cfg.dumpDir, 0o755); err != nil {
		log.Printf("backup: failed to create dump dir %q: %v", cfg.dumpDir, err)
		return
	}

	log.Printf("backup: dump dir=%q interval=%s schemas=%s retries=%d", cfg.dumpDir, cfg.interval, schemasLabel(cfg), cfg.maxRetries)

	if err := runSchemaBackup(ctx, cfg, databaseURL); err != nil {
		log.Printf("backup: initial schema dump failed: %v", err)
	}

	go func() {
		if err := runDataBackup(ctx, cfg, databaseURL); err != nil {
			log.Printf("backup: initial data dump failed: %v", err)
		}
		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Printf("backup: context done, stopping data backup loop")
				return
			case <-ticker.C:
				if err := runDataBackup(ctx, cfg, databaseURL); err != nil {
					log.Printf("backup: scheduled data dump failed: %v", err)
				}
			}
		}
	}()
}

// runPgDump executes pg_dump with the given flags + databaseURL, writing
// stdout to outPath (overwriting). Stderr is captured and appended to any
// error message so callers see why pg_dump failed.
func runPgDump(ctx context.Context, outPath, databaseURL string, flags []string) error {
	dumpCtx, cancel := context.WithTimeout(ctx, pgDumpTimeout)
	defer cancel()

	args := append([]string{}, flags...)
	args = append(args, "--file", outPath, databaseURL)

	cmd := exec.CommandContext(dumpCtx, "pg_dump", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func dumpFlagsForSchemas(base []string, cfg backupConfig) []string {
	if cfg.allSchemas {
		return base
	}
	out := append([]string{}, base...)
	for _, s := range cfg.schemas {
		out = append(out, "--schema", s)
	}
	return out
}

func runSchemaBackup(ctx context.Context, cfg backupConfig, databaseURL string) error {
	finalPath := filepath.Join(cfg.dumpDir, schemaDumpFilename)
	tmpPath := finalPath + ".tmp"

	flags := dumpFlagsForSchemas([]string{"--schema-only", "--no-owner", "--no-acl"}, cfg)

	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("backup: schema retry %d/%d", attempt, cfg.maxRetries)
		}

		if err := runPgDump(ctx, tmpPath, databaseURL, flags); err != nil {
			lastErr = fmt.Errorf("pg_dump schema: %w", err)
			_ = os.Remove(tmpPath)
			continue
		}

		dumpTables, err := parseSchemaTableCount(tmpPath)
		if err != nil {
			lastErr = fmt.Errorf("parse schema: %w", err)
			continue
		}
		liveTables, err := liveTableCount(ctx, cfg)
		if err != nil {
			lastErr = fmt.Errorf("live table count: %w", err)
			if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
				log.Printf("backup: rename schema tmp failed: %v", renameErr)
			}
			return lastErr
		}

		if dumpTables == liveTables {
			if err := os.Rename(tmpPath, finalPath); err != nil {
				return fmt.Errorf("rename schema tmp: %w", err)
			}
			log.Printf("backup: schema dump verified (%d tables) -> %s", dumpTables, finalPath)
			return nil
		}

		lastErr = fmt.Errorf("schema verify mismatch: dump=%d live=%d", dumpTables, liveTables)
		log.Printf("backup: %v (attempt %d/%d)", lastErr, attempt+1, cfg.maxRetries+1)
	}

	if _, statErr := os.Stat(tmpPath); statErr == nil {
		if err := os.Rename(tmpPath, finalPath); err != nil {
			log.Printf("backup: WARN rename schema tmp after exhausted retries: %v", err)
		} else {
			log.Printf("backup: WARN schema dump kept despite mismatch -> %s (%v)", finalPath, lastErr)
		}
	}
	return lastErr
}

func runDataBackup(ctx context.Context, cfg backupConfig, databaseURL string) error {
	finalPath := filepath.Join(cfg.dumpDir, dataDumpFilename)
	tmpPath := finalPath + ".tmp"

	flags := dumpFlagsForSchemas([]string{"--data-only", "--no-owner", "--no-acl"}, cfg)

	start := time.Now()

	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("backup: data retry %d/%d (sleeping %s)", attempt, cfg.maxRetries, verifyPostDumpSleep)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(verifyPostDumpSleep):
			}
		}

		if err := runPgDump(ctx, tmpPath, databaseURL, flags); err != nil {
			lastErr = fmt.Errorf("pg_dump data: %w", err)
			_ = os.Remove(tmpPath)
			continue
		}

		dumpCounts, err := parseDumpRowCounts(tmpPath)
		if err != nil {
			lastErr = fmt.Errorf("parse dump: %w", err)
			continue
		}

		liveCounts, err := liveRowCounts(ctx, cfg)
		if err != nil {
			lastErr = fmt.Errorf("live row counts: %w", err)
			if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
				log.Printf("backup: rename data tmp failed: %v", renameErr)
			}
			return lastErr
		}

		diff := diffRowCounts(dumpCounts, liveCounts, cfg.rowTolerance)
		if len(diff) == 0 {
			if err := os.Rename(tmpPath, finalPath); err != nil {
				return fmt.Errorf("rename data tmp: %w", err)
			}
			total := 0
			for _, n := range dumpCounts {
				total += n
			}
			log.Printf("backup: data dump verified (%d tables, %d rows, %s) -> %s",
				len(dumpCounts), total, time.Since(start).Round(time.Millisecond), finalPath)
			return nil
		}

		lastErr = fmt.Errorf("data verify mismatch: %s", strings.Join(diff, "; "))
		log.Printf("backup: %v (attempt %d/%d)", lastErr, attempt+1, cfg.maxRetries+1)
	}

	if _, statErr := os.Stat(tmpPath); statErr == nil {
		if err := os.Rename(tmpPath, finalPath); err != nil {
			log.Printf("backup: WARN rename data tmp after exhausted retries: %v", err)
		} else {
			log.Printf("backup: WARN data dump kept despite mismatch -> %s (%v)", finalPath, lastErr)
		}
	}
	return lastErr
}

// parseDumpRowCounts walks a pg_dump SQL file (default COPY format) and
// returns a map of "schema.table" -> row count, where the row count is the
// number of data lines between `COPY ... FROM stdin;` and the terminating
// `\.` marker. Empty COPY blocks (no data) yield count = 0.
func parseDumpRowCounts(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// pg_dump can emit very long lines (TEXT/JSONB columns). Bump the buffer
	// so the scanner does not blow up on bufio.ErrTooLong for large rows.
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)

	counts := make(map[string]int)
	copyRe := regexp.MustCompile(`^COPY\s+([^\s(]+)\s*\(`)

	currentTable := ""
	for scanner.Scan() {
		line := scanner.Text()
		if currentTable == "" {
			if m := copyRe.FindStringSubmatch(line); m != nil {
				currentTable = unquoteTableName(m[1])
				if _, ok := counts[currentTable]; !ok {
					counts[currentTable] = 0
				}
			}
			continue
		}
		if line == `\.` {
			currentTable = ""
			continue
		}
		counts[currentTable]++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// unquoteTableName strips double-quotes around schema or table identifiers
// and ensures a "schema.table" form is returned. pg_dump emits unquoted
// identifiers when safe (e.g. `public.applications`) and quoted when not.
func unquoteTableName(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) {
			p = p[1 : len(p)-1]
			p = strings.ReplaceAll(p, `""`, `"`)
		}
		parts[i] = p
	}
	if len(parts) == 1 {
		return "public." + parts[0]
	}
	return strings.Join(parts, ".")
}

// parseSchemaTableCount counts CREATE TABLE statements in the schema dump.
func parseSchemaTableCount(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	re := regexp.MustCompile(`^\s*CREATE TABLE\s`)
	count := 0
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

// liveRowCounts queries information_schema for tables in the configured
// schemas and runs SELECT count(*) on each, returning "schema.table" -> count.
func liveRowCounts(ctx context.Context, cfg backupConfig) (map[string]int, error) {
	tables, err := listLiveTables(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(tables))
	for _, t := range tables {
		var n int
		q := fmt.Sprintf(`SELECT count(*) FROM %s`, quoteQualified(t.schema, t.table))
		if err := dbPool.QueryRow(ctx, q).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s.%s: %w", t.schema, t.table, err)
		}
		out[t.schema+"."+t.table] = n
	}
	return out, nil
}

func liveTableCount(ctx context.Context, cfg backupConfig) (int, error) {
	tables, err := listLiveTables(ctx, cfg)
	if err != nil {
		return 0, err
	}
	return len(tables), nil
}

type liveTable struct {
	schema string
	table  string
}

func listLiveTables(ctx context.Context, cfg backupConfig) ([]liveTable, error) {
	var rows pgx.Rows
	var err error
	if cfg.allSchemas {
		rows, err = dbPool.Query(ctx, `
SELECT table_schema, table_name FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
  AND table_schema NOT IN ('pg_catalog','information_schema')
ORDER BY table_schema, table_name`)
	} else {
		rows, err = dbPool.Query(ctx, `
SELECT table_schema, table_name FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
  AND table_schema = ANY($1)
ORDER BY table_schema, table_name`, cfg.schemas)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []liveTable{}
	for rows.Next() {
		var t liveTable
		if err := rows.Scan(&t.schema, &t.table); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteQualified(schema, table string) string {
	return quoteIdent(schema) + "." + quoteIdent(table)
}

// diffRowCounts returns human-readable diff strings for any table whose
// dump count differs from the live count by more than the tolerance, or that
// is missing on either side. Empty live tables that are missing from the dump
// are silently accepted, since a 0/0 outcome is not a real mismatch.
func diffRowCounts(dump, live map[string]int, tolerance int) []string {
	if tolerance < 0 {
		tolerance = 0
	}
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}

	diffs := []string{}
	seen := map[string]bool{}
	for k, dn := range dump {
		seen[k] = true
		ln, ok := live[k]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: extra in dump (rows=%d)", k, dn))
			continue
		}
		if abs(dn-ln) > tolerance {
			diffs = append(diffs, fmt.Sprintf("%s: dump=%d live=%d delta=%d", k, dn, ln, dn-ln))
		}
	}
	for k, ln := range live {
		if seen[k] {
			continue
		}
		if ln == 0 {
			continue
		}
		diffs = append(diffs, fmt.Sprintf("%s: missing in dump (live rows=%d)", k, ln))
	}
	if len(diffs) > 1 {
		sort.Strings(diffs)
	}
	return diffs
}
