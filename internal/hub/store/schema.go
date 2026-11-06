package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// The schema snapshots: what the migrations produce, per dialect, kept in the repository. `hub schema snapshot`
// writes them (make hub-schema); TestSchemaSnapshots fails when a migration changes the schema and they were not
// regenerated, and the hub checks the live database against them after migrating (Options.Check).
//
//go:embed schema/*.json
var snapshotFiles embed.FS

// Schema is the structure of a database: the tables with their columns, primary keys and indexes.
type Schema struct {
	Driver Driver           `json:"driver"`
	Tables map[string]Table `json:"tables"`
}

// Table is one table. Column types are the dialect's own, normalized: information_schema's data_type on Postgres,
// the declared type in upper case on SQLite.
type Table struct {
	Columns    []Column `json:"columns"`
	PrimaryKey []string `json:"primaryKey,omitempty"`
	Indexes    []Index  `json:"indexes,omitempty"`
}

// Column is one column of a table, in the order the table was created.
type Column struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"notNull,omitempty"`
}

// Index is a secondary index. Name is empty for the index behind a UNIQUE constraint, which the databases name
// differently.
type Index struct {
	Name    string   `json:"name,omitempty"`
	Unique  bool     `json:"unique,omitempty"`
	Columns []string `json:"columns"`
}

func (i Index) key() string {
	if i.Name != "" {
		return "index " + i.Name
	}
	return "unique(" + strings.Join(i.Columns, ",") + ")"
}

// ExpectedSchema is the snapshot kept in the repository for a dialect.
func ExpectedSchema(driver Driver) (Schema, error) {
	var s Schema
	b, err := snapshotFiles.ReadFile("schema/" + string(driver) + ".json")
	if err != nil {
		return s, fmt.Errorf("no schema snapshot for %s: %w", driver, err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("schema snapshot %s: %w", driver, err)
	}
	return s, nil
}

// MarshalSchema is the stable text form of a snapshot (the file in the repository).
func MarshalSchema(s Schema) ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ") // map keys are sorted
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// IntrospectSchema reads the structure of the tables of the current schema (Postgres) or the file (SQLite).
func IntrospectSchema(ctx context.Context, q Q, driver Driver) (Schema, error) {
	s := Schema{Driver: driver, Tables: map[string]Table{}}
	var err error
	if driver == Postgres {
		err = introspectPostgres(ctx, q, &s)
	} else {
		err = introspectSQLite(ctx, q, &s)
	}
	return s, err
}

func introspectPostgres(ctx context.Context, q Q, s *Schema) error {
	rows, err := q.Query(ctx, `SELECT table_name, column_name, data_type, is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() ORDER BY table_name, ordinal_position`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var table, col, typ, nullable string
		if err := rows.Scan(&table, &col, &typ, &nullable); err != nil {
			rows.Close()
			return err
		}
		t := s.Tables[table]
		t.Columns = append(t.Columns, Column{Name: col, Type: typ, NotNull: nullable == "NO"})
		s.Tables[table] = t
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	// Views are in information_schema.columns too; keep the base tables.
	trows, err := q.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
	if err != nil {
		return err
	}
	base := map[string]bool{}
	for trows.Next() {
		var n string
		if err := trows.Scan(&n); err != nil {
			trows.Close()
			return err
		}
		base[n] = true
	}
	if err := closeRows(trows); err != nil {
		return err
	}
	for n := range s.Tables {
		if !base[n] {
			delete(s.Tables, n)
		}
	}

	pk, err := q.Query(ctx, `SELECT c.relname, a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		WHERE i.indisprimary AND n.nspname = current_schema()
		ORDER BY c.relname, k.ord`)
	if err != nil {
		return err
	}
	for pk.Next() {
		var table, col string
		if err := pk.Scan(&table, &col); err != nil {
			pk.Close()
			return err
		}
		if t, ok := s.Tables[table]; ok {
			t.PrimaryKey = append(t.PrimaryKey, col)
			s.Tables[table] = t
		}
	}
	if err := closeRows(pk); err != nil {
		return err
	}

	ix, err := q.Query(ctx, `SELECT t.relname, ic.relname, i.indisunique, (con.oid IS NOT NULL), a.attname
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		LEFT JOIN pg_constraint con ON con.conindid = i.indexrelid AND con.contype IN ('p', 'u')
		CROSS JOIN LATERAL unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = current_schema() AND NOT i.indisprimary
		ORDER BY t.relname, ic.relname, k.ord`)
	if err != nil {
		return err
	}
	type pending struct {
		table string
		idx   Index
	}
	var order []string
	byName := map[string]*pending{}
	for ix.Next() {
		var table, name, col string
		var unique, isConstraint bool
		if err := ix.Scan(&table, &name, &unique, &isConstraint, &col); err != nil {
			ix.Close()
			return err
		}
		p, ok := byName[name]
		if !ok {
			p = &pending{table: table, idx: Index{Unique: unique}}
			if !isConstraint {
				p.idx.Name = name
			}
			byName[name] = p
			order = append(order, name)
		}
		p.idx.Columns = append(p.idx.Columns, col)
	}
	if err := closeRows(ix); err != nil {
		return err
	}
	for _, name := range order {
		p := byName[name]
		if t, ok := s.Tables[p.table]; ok {
			t.Indexes = append(t.Indexes, p.idx)
			s.Tables[p.table] = t
		}
	}
	sortIndexes(s)
	return nil
}

func introspectSQLite(ctx context.Context, q Q, s *Schema) error {
	rows, err := q.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, n)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, name := range tables {
		t := Table{}
		cols, err := q.Query(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info(?) ORDER BY cid`, name)
		if err != nil {
			return err
		}
		type pkcol struct {
			name string
			pos  int
		}
		var pks []pkcol
		for cols.Next() {
			var c Column
			var notnull, pk int
			if err := cols.Scan(&c.Name, &c.Type, &notnull, &pk); err != nil {
				cols.Close()
				return err
			}
			c.Type = strings.ToUpper(c.Type)
			c.NotNull = notnull == 1 || pk > 0
			t.Columns = append(t.Columns, c)
			if pk > 0 {
				pks = append(pks, pkcol{c.Name, pk})
			}
		}
		if err := closeRows(cols); err != nil {
			return err
		}
		sort.Slice(pks, func(i, j int) bool { return pks[i].pos < pks[j].pos })
		for _, p := range pks {
			t.PrimaryKey = append(t.PrimaryKey, p.name)
		}

		il, err := q.Query(ctx, `SELECT name, "unique", origin FROM pragma_index_list(?) WHERE origin <> 'pk' ORDER BY name`, name)
		if err != nil {
			return err
		}
		type ixrow struct {
			name   string
			unique bool
			origin string
		}
		var ixs []ixrow
		for il.Next() {
			var r ixrow
			var u int
			if err := il.Scan(&r.name, &u, &r.origin); err != nil {
				il.Close()
				return err
			}
			r.unique = u == 1
			ixs = append(ixs, r)
		}
		if err := closeRows(il); err != nil {
			return err
		}
		for _, r := range ixs {
			ic, err := q.Query(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, r.name)
			if err != nil {
				return err
			}
			idx := Index{Unique: r.unique}
			if r.origin == "c" {
				idx.Name = r.name
			}
			for ic.Next() {
				var c string
				if err := ic.Scan(&c); err != nil {
					ic.Close()
					return err
				}
				idx.Columns = append(idx.Columns, c)
			}
			if err := closeRows(ic); err != nil {
				return err
			}
			t.Indexes = append(t.Indexes, idx)
		}
		s.Tables[name] = t
	}
	sortIndexes(s)
	return nil
}

func closeRows(r *sql.Rows) error {
	if err := r.Err(); err != nil {
		r.Close()
		return err
	}
	return r.Close()
}

func sortIndexes(s *Schema) {
	for n, t := range s.Tables {
		sort.Slice(t.Indexes, func(i, j int) bool { return t.Indexes[i].key() < t.Indexes[j].key() })
		s.Tables[n] = t
	}
}

// DiffSchema lists what is missing or different in actual compared with expected. What actual has extra (the tables of
// a hub that is newer and compatible, an index an operator added) is not a difference.
func DiffSchema(expected, actual Schema) []string {
	var out []string
	names := make([]string, 0, len(expected.Tables))
	for n := range expected.Tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		want := expected.Tables[n]
		have, ok := actual.Tables[n]
		if !ok {
			out = append(out, "table "+n+" is missing")
			continue
		}
		cols := map[string]Column{}
		for _, c := range have.Columns {
			cols[c.Name] = c
		}
		for _, c := range want.Columns {
			h, ok := cols[c.Name]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("column %s.%s is missing", n, c.Name))
			case h.Type != c.Type:
				out = append(out, fmt.Sprintf("column %s.%s has type %s, expected %s", n, c.Name, h.Type, c.Type))
			case h.NotNull != c.NotNull:
				out = append(out, fmt.Sprintf("column %s.%s: NOT NULL is %t, expected %t", n, c.Name, h.NotNull, c.NotNull))
			}
		}
		if strings.Join(want.PrimaryKey, ",") != strings.Join(have.PrimaryKey, ",") {
			out = append(out, fmt.Sprintf("table %s has primary key (%s), expected (%s)", n, strings.Join(have.PrimaryKey, ","), strings.Join(want.PrimaryKey, ",")))
		}
		idx := map[string]Index{}
		for _, i := range have.Indexes {
			idx[i.key()] = i
		}
		for _, w := range want.Indexes {
			h, ok := idx[w.key()]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("table %s: %s is missing", n, w.key()))
			case h.Unique != w.Unique || strings.Join(h.Columns, ",") != strings.Join(w.Columns, ","):
				out = append(out, fmt.Sprintf("table %s: %s covers (%s) unique=%t, expected (%s) unique=%t", n, w.key(), strings.Join(h.Columns, ","), h.Unique, strings.Join(w.Columns, ","), w.Unique))
			}
		}
	}
	return out
}

// VerifySchema compares the live schema with the snapshot of the repository and lists the differences.
func VerifySchema(ctx context.Context, q Q, driver Driver) ([]string, error) {
	want, err := ExpectedSchema(driver)
	if err != nil {
		return nil, err
	}
	have, err := IntrospectSchema(ctx, q, driver)
	if err != nil {
		return nil, err
	}
	return DiffSchema(want, have), nil
}

// SnapshotSchema migrates an empty database and reads its schema: what `hub schema snapshot` writes. A SQLite URL
// is used as given (it must be a new file); a Postgres URL gets a throwaway schema that is dropped afterwards.
func SnapshotSchema(ctx context.Context, databaseURL string) (Schema, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return Schema{}, errors.New("database url: cannot parse")
	}
	opts := Options{Check: CheckOff}
	driver := SQLite
	var cleanup func()
	if u.Scheme == "postgres" || u.Scheme == "postgresql" {
		driver = Postgres
		admin, err := connectPostgres(databaseURL, Options{}, false)
		if err != nil {
			return Schema{}, err
		}
		defer admin.Close()
		opts.Schema = "hubsnap_" + strings.ToLower(RandomBase32(6))
		cleanup = func() {
			_, _ = admin.ExecContext(context.WithoutCancel(ctx), `DROP SCHEMA IF EXISTS "`+opts.Schema+`" CASCADE`)
		}
		defer cleanup()
	}
	db, err := OpenWith(ctx, databaseURL, opts)
	if err != nil {
		return Schema{}, err
	}
	defer db.Close()
	return IntrospectSchema(ctx, db, driver)
}
