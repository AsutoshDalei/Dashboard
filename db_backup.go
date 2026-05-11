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

	cfg := loadBackupConfig()
	if err := os.MkdirAll(cfg.dumpDir, 0o755); err != nil {
		log.Printf("backup: failed to create dump dir %q: %v", cfg.dumpDir, err)
		return
	}

	log.Printf("backup: dump dir=%q interval=%s schemas=%s retries=%d (data=native COPY)", cfg.dumpDir, cfg.interval, schemasLabel(cfg), cfg.maxRetries)

	if _, err := exec.LookPath("pg_dump"); err != nil {
		log.Printf("backup: pg_dump not found in PATH; schema.sql skipped — use migrations or install postgresql-client for schema dumps (%v)", err)
	} else if err := runSchemaBackup(ctx, cfg, databaseURL); err != nil {
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

func listTableColumns(ctx context.Context, schema, table string) ([]string, error) {
	rows, err := dbPool.Query(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// listFKParentBeforeChildEdges returns (parentKey, childKey) pairs where parentKey is the
// referenced table and childKey the dependent; parent must appear before child in the dump
// so a straight psql restore satisfies foreign keys.
func listFKParentBeforeChildEdges(ctx context.Context, cfg backupConfig, inBackup map[string]bool) ([][2]string, error) {
	var rows pgx.Rows
	var err error
	if cfg.allSchemas {
		rows, err = dbPool.Query(ctx, `
SELECT rn.nspname::text, r.relname::text, n.nspname::text, c.relname::text
FROM pg_constraint con
JOIN pg_class c ON con.conrelid = c.oid
JOIN pg_namespace n ON c.relnamespace = n.oid
JOIN pg_class r ON con.confrelid = r.oid
JOIN pg_namespace rn ON r.relnamespace = rn.oid
WHERE con.contype = 'f'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND rn.nspname NOT IN ('pg_catalog', 'information_schema')`)
	} else {
		rows, err = dbPool.Query(ctx, `
SELECT rn.nspname::text, r.relname::text, n.nspname::text, c.relname::text
FROM pg_constraint con
JOIN pg_class c ON con.conrelid = c.oid
JOIN pg_namespace n ON c.relnamespace = n.oid
JOIN pg_class r ON con.confrelid = r.oid
JOIN pg_namespace rn ON r.relnamespace = rn.oid
WHERE con.contype = 'f'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND rn.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname = ANY($1::text[])
  AND rn.nspname = ANY($1::text[])`, cfg.schemas)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges [][2]string
	for rows.Next() {
		var refSchema, refTable, depSchema, depTable string
		if err := rows.Scan(&refSchema, &refTable, &depSchema, &depTable); err != nil {
			return nil, err
		}
		parentKey := refSchema + "." + refTable
		childKey := depSchema + "." + depTable
		if parentKey == childKey {
			continue
		}
		if !inBackup[parentKey] || !inBackup[childKey] {
			continue
		}
		edges = append(edges, [2]string{parentKey, childKey})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}

func topoSortTables(tables []liveTable, edges [][2]string) []liveTable {
	key := func(t liveTable) string { return t.schema + "." + t.table }
	keys := make([]string, 0, len(tables))
	idx := make(map[string]liveTable, len(tables))
	for _, t := range tables {
		k := key(t)
		idx[k] = t
		keys = append(keys, k)
	}
	sort.Strings(keys)

	indegree := make(map[string]int, len(keys))
	adj := make(map[string][]string)
	for _, k := range keys {
		indegree[k] = 0
	}
	for _, e := range edges {
		p, c := e[0], e[1]
		if _, ok := indegree[p]; !ok {
			continue
		}
		if _, ok := indegree[c]; !ok {
			continue
		}
		adj[p] = append(adj[p], c)
		indegree[c]++
	}

	queue := make([]string, 0)
	for _, k := range keys {
		if indegree[k] == 0 {
			queue = append(queue, k)
		}
	}
	sort.Strings(queue)

	out := make([]liveTable, 0, len(tables))
	processed := 0
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		out = append(out, idx[u])
		processed++
		neighbors := append([]string(nil), adj[u]...)
		sort.Strings(neighbors)
		for _, v := range neighbors {
			indegree[v]--
			if indegree[v] == 0 {
				queue = append(queue, v)
				sort.Strings(queue)
			}
		}
	}
	if processed < len(tables) {
		seen := make(map[string]bool, len(out))
		for _, t := range out {
			seen[key(t)] = true
		}
		var rest []string
		for _, k := range keys {
			if !seen[k] {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		for _, k := range rest {
			out = append(out, idx[k])
		}
	}
	return out
}

func runNativeDataDump(ctx context.Context, cfg backupConfig, outPath string) (err error) {
	dumpCtx, cancel := context.WithTimeout(ctx, pgDumpTimeout)
	defer cancel()

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 256*1024)
	defer func() {
		flushErr := bw.Flush()
		closeErr := f.Close()
		if err == nil {
			if flushErr != nil {
				err = flushErr
			} else if closeErr != nil {
				err = closeErr
			}
		}
		if err != nil {
			_ = os.Remove(outPath)
		}
	}()

	tables, err := listLiveTables(dumpCtx, cfg)
	if err != nil {
		return err
	}
	inBackup := make(map[string]bool, len(tables))
	for _, t := range tables {
		inBackup[t.schema+"."+t.table] = true
	}
	edges, err := listFKParentBeforeChildEdges(dumpCtx, cfg, inBackup)
	if err != nil {
		return err
	}
	sorted := topoSortTables(tables, edges)

	conn, err := dbPool.Acquire(dumpCtx)
	if err != nil {
		return err
	}
	defer conn.Release()
	pgConn := conn.Conn().PgConn()

	for _, t := range sorted {
		cols, err := listTableColumns(dumpCtx, t.schema, t.table)
		if err != nil {
			return fmt.Errorf("columns %s.%s: %w", t.schema, t.table, err)
		}
		if len(cols) == 0 {
			return fmt.Errorf("no columns for %s.%s", t.schema, t.table)
		}
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			quotedCols[i] = quoteIdent(c)
		}
		q := quoteQualified(t.schema, t.table)
		header := fmt.Sprintf("COPY %s (%s) FROM stdin;\n", q, strings.Join(quotedCols, ", "))
		if _, err := bw.WriteString(header); err != nil {
			return err
		}
		copySQL := fmt.Sprintf("COPY %s (%s) TO STDOUT", q, strings.Join(quotedCols, ", "))
		if _, err := pgConn.CopyTo(dumpCtx, bw, copySQL); err != nil {
			return fmt.Errorf("COPY %s.%s: %w", t.schema, t.table, err)
		}
		if _, err := bw.WriteString("\\.\n"); err != nil {
			return err
		}
	}

	return nil
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
	_ = databaseURL // native export uses dbPool
	finalPath := filepath.Join(cfg.dumpDir, dataDumpFilename)
	tmpPath := finalPath + ".tmp"

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

		if err := runNativeDataDump(ctx, cfg, tmpPath); err != nil {
			lastErr = fmt.Errorf("native data dump: %w", err)
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
