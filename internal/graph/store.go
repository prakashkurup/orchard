package graph

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGo)
)

// Store is the per-repo SQLite graph database. It owns the schema and the
// queries; orchestration lives in Graph (graph.go).
type Store struct {
	db  *sql.DB
	fts bool // FTS5 trigram index available (fast substring symbol search)
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS files (
  id     INTEGER PRIMARY KEY,
  path   TEXT NOT NULL UNIQUE,
  lang   TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  bytes  INTEGER NOT NULL,
  tier   TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS symbols (
  id         INTEGER PRIMARY KEY,
  file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  signature  TEXT,
  start_line INTEGER NOT NULL,
  end_line   INTEGER NOT NULL,
  rank       REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_sym_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_sym_file ON symbols(file_id);
CREATE TABLE IF NOT EXISTS edges (
  id          INTEGER PRIMARY KEY,
  src_id      INTEGER REFERENCES symbols(id) ON DELETE CASCADE,
  dst_id      INTEGER REFERENCES symbols(id),
  ref_file_id INTEGER NOT NULL REFERENCES files(id),
  dst_name    TEXT NOT NULL,
  kind        TEXT NOT NULL,
  confidence  TEXT NOT NULL,
  line        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_edge_dst ON edges(dst_id);
CREATE INDEX IF NOT EXISTS idx_edge_src ON edges(src_id);
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// openStore opens (creating if needed) the SQLite graph at path, in WAL mode so
// the agent can read while a reindex writes.
func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	// Optional FTS5 trigram index for fast, case-insensitive substring symbol
	// search. If the SQLite build lacks FTS5/trigram, fall back to LIKE.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS symbols_fts USING fts5(name, sid UNINDEXED, tokenize="trigram case_sensitive 0")`); err == nil {
		s.fts = true
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// replace rebuilds the whole graph from files in one transaction: it clears the
// tables, inserts files+symbols, then resolves each edge's source (enclosing
// symbol) and destination by name — same-file first, then any matching
// definition — labelling the edge extracted / ambiguous / inferred.
func (s *Store) replace(files []FileGraph) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless Commit succeeds

	for _, table := range []string{"edges", "symbols", "files"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
	}
	if s.fts {
		if _, err := tx.Exec("DELETE FROM symbols_fts"); err != nil {
			return err
		}
	}

	fileID := make(map[string]int64, len(files))
	for _, fg := range files {
		res, err := tx.Exec(
			`INSERT INTO files(path,lang,sha256,bytes,tier) VALUES(?,?,?,?,?)`,
			fg.File.Path, fg.File.Lang, fg.File.SHA, fg.File.Bytes, string(fg.File.Tier))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		fileID[fg.File.Path] = id
	}

	byName := map[string][]int64{}
	byFileName := map[string]int64{}
	for _, fg := range files {
		fid := fileID[fg.File.Path]
		for _, sym := range fg.Symbols {
			res, err := tx.Exec(
				`INSERT INTO symbols(file_id,name,kind,signature,start_line,end_line) VALUES(?,?,?,?,?,?)`,
				fid, sym.Name, string(sym.Kind), sym.Signature, sym.StartLine, sym.EndLine)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			byName[sym.Name] = append(byName[sym.Name], id)
			byFileName[fg.File.Path+"\x00"+sym.Name] = id
			if s.fts {
				if _, err := tx.Exec(`INSERT INTO symbols_fts(name, sid) VALUES(?,?)`, sym.Name, id); err != nil {
					return err
				}
			}
		}
	}

	// resolve returns the best symbol id for name as referenced in path.
	resolve := func(path, name string) (id int64, found, ambiguous bool) {
		if id, ok := byFileName[path+"\x00"+name]; ok {
			return id, true, false
		}
		ids := byName[name]
		switch len(ids) {
		case 0:
			return 0, false, false
		case 1:
			return ids[0], true, false
		default:
			return ids[0], true, true
		}
	}

	for _, fg := range files {
		fid := fileID[fg.File.Path]
		for _, e := range fg.Edges {
			var srcID, dstID any
			if id, ok, _ := resolve(fg.File.Path, e.SrcName); ok {
				srcID = id
			}
			conf := Inferred
			if id, ok, amb := resolve(fg.File.Path, e.DstName); ok {
				dstID = id
				conf = Extracted
				if amb {
					conf = Ambiguous
				}
			}
			if _, err := tx.Exec(
				`INSERT INTO edges(src_id,dst_id,ref_file_id,dst_name,kind,confidence,line) VALUES(?,?,?,?,?,?,?)`,
				srcID, dstID, fid, e.DstName, string(e.Kind), string(conf), e.Line); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// snapshot returns the stored per-file metadata keyed by path — used to diff
// against the working tree for incremental reindex.
func (s *Store) snapshot() (map[string]fileMeta, error) {
	rows, err := s.db.Query(`SELECT path, lang, sha256, bytes, tier FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]fileMeta{}
	for rows.Next() {
		var f fileMeta
		var tier string
		if err := rows.Scan(&f.Path, &f.Lang, &f.SHA, &f.Bytes, &tier); err != nil {
			return nil, err
		}
		f.Tier = Tier(tier)
		out[f.Path] = f
	}
	return out, rows.Err()
}

// reconstruct rebuilds FileGraphs (metadata + symbols + raw-named edges) from the
// DB for the kept files, so an incremental update can re-resolve the whole graph
// without re-parsing unchanged files.
func (s *Store) reconstruct(keep map[string]fileMeta) ([]FileGraph, error) {
	symsByFile := map[string][]Symbol{}
	srows, err := s.db.Query(
		`SELECT f.path, s.name, s.kind, COALESCE(s.signature,''), s.start_line, s.end_line
		   FROM symbols s JOIN files f ON f.id=s.file_id`)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var path, kind string
		var sym Symbol
		if err := srows.Scan(&path, &sym.Name, &kind, &sym.Signature, &sym.StartLine, &sym.EndLine); err != nil {
			srows.Close()
			return nil, err
		}
		sym.Kind = Kind(kind)
		if _, ok := keep[path]; ok {
			symsByFile[path] = append(symsByFile[path], sym)
		}
	}
	srows.Close()

	edgesByFile := map[string][]Edge{}
	erows, err := s.db.Query(
		`SELECT f.path, COALESCE(cs.name,''), e.dst_name, e.kind, e.line
		   FROM edges e JOIN files f ON f.id=e.ref_file_id
		   LEFT JOIN symbols cs ON cs.id=e.src_id`)
	if err != nil {
		return nil, err
	}
	for erows.Next() {
		var path, kind string
		var e Edge
		if err := erows.Scan(&path, &e.SrcName, &e.DstName, &kind, &e.Line); err != nil {
			erows.Close()
			return nil, err
		}
		e.Kind = EdgeKind(kind)
		if _, ok := keep[path]; ok {
			edgesByFile[path] = append(edgesByFile[path], e)
		}
	}
	erows.Close()

	out := make([]FileGraph, 0, len(keep))
	for path, fm := range keep {
		out = append(out, FileGraph{File: fm, Symbols: symsByFile[path], Edges: edgesByFile[path]})
	}
	return out, nil
}

// setMeta upserts key/value freshness metadata.
func (s *Store) setMeta(kv map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.Exec(
			`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// getMeta returns a meta value, or "" if absent.
func (s *Store) getMeta(key string) string {
	var v string
	s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

// rankPass computes PageRank over the resolved call graph and writes a rank onto
// every symbol, so the repo map and result ordering can surface important code
// first. Run after replace().
func (s *Store) rankPass() error {
	var nodes []int64
	nrows, err := s.db.Query(`SELECT id FROM symbols`)
	if err != nil {
		return err
	}
	for nrows.Next() {
		var id int64
		if err := nrows.Scan(&id); err != nil {
			nrows.Close()
			return err
		}
		nodes = append(nodes, id)
	}
	nrows.Close()

	edges := map[int64][]int64{}
	erows, err := s.db.Query(`SELECT src_id, dst_id FROM edges WHERE src_id IS NOT NULL AND dst_id IS NOT NULL`)
	if err != nil {
		return err
	}
	for erows.Next() {
		var src, dst int64
		if err := erows.Scan(&src, &dst); err != nil {
			erows.Close()
			return err
		}
		edges[src] = append(edges[src], dst)
	}
	erows.Close()

	scores := pageRank(nodes, edges, 0.85, 30)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.Prepare(`UPDATE symbols SET rank=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for id, sc := range scores {
		if _, err := stmt.Exec(sc, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MapRow is one entry in the ranked repo map.
type MapRow struct {
	Path, Kind, Name, Signature string
	Rank                        float64
}

// repoMap returns the top-ranked definitions (a PageRank-ordered skeleton),
// capped at limit — the token-budgeted overview an agent reads instead of the
// whole tree.
func (s *Store) repoMap(limit int) ([]MapRow, error) {
	rows, err := s.db.Query(
		`SELECT f.path, s.kind, s.name, COALESCE(s.signature,''), s.rank
		   FROM symbols s JOIN files f ON f.id=s.file_id
		  WHERE s.kind IN ('function','method','class','struct','interface','type','module')
		  ORDER BY s.rank DESC, f.path, s.start_line LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MapRow
	for rows.Next() {
		var m MapRow
		if err := rows.Scan(&m.Path, &m.Kind, &m.Name, &m.Signature, &m.Rank); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// likeEscape escapes LIKE wildcards in a user query (used with ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// searchSymbols finds definitions whose name contains query (case-insensitive),
// ranked by importance, capped at limit. For queries of 3+ chars it uses the
// FTS5 trigram index when available (fast substring); otherwise it falls back to
// a LIKE scan.
func (s *Store) searchSymbols(query string, limit int) ([]DefRow, error) {
	scan := func(rows *sql.Rows, err error) ([]DefRow, error) {
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []DefRow
		for rows.Next() {
			var d DefRow
			if err := rows.Scan(&d.Name, &d.Kind, &d.Signature, &d.Path, &d.Line, &d.Rank); err != nil {
				return nil, err
			}
			out = append(out, d)
		}
		return out, rows.Err()
	}

	if s.fts && len(query) >= 3 {
		out, err := scan(s.db.Query(
			`SELECT s.name, s.kind, COALESCE(s.signature,''), f.path, s.start_line, s.rank
			   FROM symbols_fts ft JOIN symbols s ON s.id=ft.sid JOIN files f ON f.id=s.file_id
			  WHERE ft MATCH ? ORDER BY s.rank DESC, s.name LIMIT ?`, ftsPhrase(query), limit))
		if err == nil {
			return out, nil // fall through to LIKE only on FTS error
		}
	}
	return scan(s.db.Query(
		`SELECT s.name, s.kind, COALESCE(s.signature,''), f.path, s.start_line, s.rank
		   FROM symbols s JOIN files f ON f.id=s.file_id
		  WHERE s.name LIKE ? ESCAPE '\'
		  ORDER BY s.rank DESC, s.name LIMIT ?`, "%"+likeEscape(query)+"%", limit))
}

// ftsPhrase wraps a query as an FTS5 string literal (a "phrase" → substring with
// the trigram tokenizer), escaping embedded double quotes.
func ftsPhrase(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

// counts returns row totals plus how many edges resolved to a definition.
func (s *Store) counts() (files, symbols, edges, resolvedEdges int) {
	s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&files)
	s.db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&symbols)
	s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edges)
	s.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE dst_id IS NOT NULL`).Scan(&resolvedEdges)
	return
}

// tierCounts returns how many indexed files came from each parser quality tier.
func (s *Store) tierCounts() (map[Tier]int, error) {
	rows, err := s.db.Query(`SELECT tier, COUNT(*) FROM files GROUP BY tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[Tier]int{}
	for rows.Next() {
		var tier string
		var count int
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, err
		}
		out[Tier(tier)] = count
	}
	return out, rows.Err()
}

// trustLabels returns parser quality grouped by language, sorted for display.
func (s *Store) trustLabels() ([]LangTrust, error) {
	rows, err := s.db.Query(`SELECT lang, tier, COUNT(*) FROM files GROUP BY lang, tier ORDER BY lang, tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LangTrust
	for rows.Next() {
		var t LangTrust
		var tier string
		if err := rows.Scan(&t.Lang, &tier, &t.Files); err != nil {
			return nil, err
		}
		t.Tier = Tier(tier)
		out = append(out, t)
	}
	return out, rows.Err()
}

// topByInbound returns the symbol names with the most inbound edges — handy for
// picking interesting symbols to demonstrate queries.
func (s *Store) topByInbound(limit int) []string {
	rows, err := s.db.Query(
		`SELECT ds.name, COUNT(*) c
		   FROM edges e JOIN symbols ds ON ds.id=e.dst_id
		  GROUP BY ds.name ORDER BY c DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		var c int
		if rows.Scan(&name, &c) == nil {
			out = append(out, name)
		}
	}
	return out
}

// DefRow is a definition site returned by FindDef.
type DefRow struct {
	Name, Kind, Signature, Path string
	Line                        int
	Rank                        float64
}

func (s *Store) findDef(name string, limit int) ([]DefRow, error) {
	rows, err := s.db.Query(
		`SELECT s.name, s.kind, COALESCE(s.signature,''), f.path, s.start_line, s.rank
		   FROM symbols s JOIN files f ON f.id=s.file_id
		  WHERE s.name=? ORDER BY s.rank DESC, f.path LIMIT ?`, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DefRow
	for rows.Next() {
		var d DefRow
		if err := rows.Scan(&d.Name, &d.Kind, &d.Signature, &d.Path, &d.Line, &d.Rank); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CallerRow is an inbound call/reference site returned by WhoCalls.
type CallerRow struct {
	Caller, Path string
	Line         int
	Rank         float64
}

func (s *Store) whoCalls(name string, limit, offset int) ([]CallerRow, error) {
	rows, err := s.db.Query(
		`SELECT COALESCE(cs.name,'(file-scope)'), f.path, e.line, COALESCE(cs.rank,0)
		   FROM edges e
		   JOIN symbols ds ON ds.id=e.dst_id
		   LEFT JOIN symbols cs ON cs.id=e.src_id
		   JOIN files f ON f.id=e.ref_file_id
		  WHERE ds.name=?
		  ORDER BY COALESCE(cs.rank,0) DESC, f.path, e.line LIMIT ? OFFSET ?`, name, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallerRow
	for rows.Next() {
		var c CallerRow
		if err := rows.Scan(&c.Caller, &c.Path, &c.Line, &c.Rank); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ImpactRow is a transitively-affected symbol returned by BlastRadius.
type ImpactRow struct {
	Name, Path string
	Depth      int
}

func (s *Store) blastRadius(name string, maxDepth, limit int) ([]ImpactRow, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE br(id, depth) AS (
		  SELECT id, 0 FROM symbols WHERE name = ?
		  UNION
		  SELECT e.src_id, br.depth+1
		    FROM edges e JOIN br ON e.dst_id = br.id
		   WHERE e.src_id IS NOT NULL AND br.depth < ?
		)
		SELECT s.name, f.path, MIN(br.depth)
		  FROM br JOIN symbols s ON s.id=br.id JOIN files f ON f.id=s.file_id
		 WHERE br.depth > 0
		 GROUP BY s.id ORDER BY 3, 2 LIMIT ?`, name, maxDepth, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImpactRow
	for rows.Next() {
		var r ImpactRow
		if err := rows.Scan(&r.Name, &r.Path, &r.Depth); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
