package core

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver" // driver database/sql "sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"  // motore SQLite incorporato (nessuna DLL esterna)
)

// Store è l'accesso al database SQLite locale.
type Store struct {
	db   *sql.DB
	path string
}

// OpenStore apre (creandolo se manca) il database SQLite e applica le migrazioni.
func OpenStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("impossibile creare la cartella dati: %w", err)
		}
	}
	dsn := "file:" + url.PathEscape(filepath.ToSlash(path)) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(wal)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("apertura database fallita: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database non raggiungibile: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	due_date   TEXT    NOT NULL,
	due_time   TEXT    NOT NULL DEFAULT '',
	category   TEXT    NOT NULL DEFAULT '',
	note       TEXT    NOT NULL DEFAULT '',
	done       INTEGER NOT NULL DEFAULT 0,
	done_at    TEXT    NOT NULL DEFAULT '',
	created_at TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_due  ON tasks(done, due_date, due_time);
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("creazione schema fallita: %w", err)
	}
	// --- versione 2: tipo attività (compiti / verifiche) e registro materie.
	// Le modifiche sono additive: i database già esistenti conservano i dati.
	if !s.hasColumn("tasks", "kind") {
		if _, err := s.db.Exec(
			`ALTER TABLE tasks ADD COLUMN kind TEXT NOT NULL DEFAULT 'compito'`); err != nil {
			return fmt.Errorf("aggiunta colonna kind fallita: %w", err)
		}
	}
	const schemaV2 = `
CREATE TABLE IF NOT EXISTS subjects (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT    NOT NULL,
	created_at TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subjects_name ON subjects(name COLLATE NOCASE);
CREATE TABLE IF NOT EXISTS grades (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	subject_id INTEGER NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
	value      REAL    NOT NULL,
	date       TEXT    NOT NULL,
	type       TEXT    NOT NULL DEFAULT '',
	note       TEXT    NOT NULL DEFAULT '',
	created_at TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_grades_subject ON grades(subject_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_kind ON tasks(kind, done, due_date);
`
	if _, err := s.db.Exec(schemaV2); err != nil {
		return fmt.Errorf("creazione registro materie fallita: %w", err)
	}

	// --- versione 3: docente della materia (facoltativo)
	if !s.hasColumn("subjects", "teacher") {
		if _, err := s.db.Exec(
			`ALTER TABLE subjects ADD COLUMN teacher TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("aggiunta colonna docente fallita: %w", err)
		}
	}

	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES('schema_version','3')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if err != nil {
		return fmt.Errorf("scrittura meta fallita: %w", err)
	}
	return nil
}

// hasColumn dice se una colonna esiste già: serve per applicare le
// migrazioni una sola volta, senza toccare i dati presenti.
func (s *Store) hasColumn(table, column string) bool {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false
		}
		if strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}

// Close chiude il database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path restituisce il percorso del file database.
func (s *Store) Path() string { return s.path }

// ErrNotFound viene restituito quando l'attività richiesta non esiste.
var ErrNotFound = errors.New("attività non trovata")

func scanTasks(rows *sql.Rows) ([]Task, error) {
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		var done int
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.DueDate, &t.DueTime, &t.Category, &t.Note, &done, &t.DoneAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Done = done != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

const selectCols = `id,kind,title,due_date,due_time,category,note,done,done_at,created_at`

// List restituisce le attività ordinate per scadenza crescente
// (prima le più vicine, a parità di data prima quelle con un orario).
func (s *Store) List(done bool) ([]Task, error) {
	d := 0
	if done {
		d = 1
	}
	rows, err := s.db.Query(`SELECT `+selectCols+` FROM tasks WHERE done=? `+
		`ORDER BY due_date ASC, (due_time='') ASC, due_time ASC, id ASC`, d)
	if err != nil {
		return nil, err
	}
	tasks, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	sortTasks(tasks)
	return tasks, nil
}

// ListAll restituisce tutte le attività (completate incluse).
func (s *Store) ListAll() ([]Task, error) {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM tasks ORDER BY due_date ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	tasks, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	sortTasks(tasks)
	return tasks, nil
}

// sortTasks garantisce l'ordinamento anche in memoria (scadenza più vicina per prima).
func sortTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.DueDate != b.DueDate {
			return a.DueDate < b.DueDate
		}
		if (a.DueTime == "") != (b.DueTime == "") {
			return a.DueTime != "" // chi ha un orario viene prima
		}
		if a.DueTime != b.DueTime {
			return a.DueTime < b.DueTime
		}
		return a.ID < b.ID
	})
}

// Get restituisce una singola attività.
func (s *Store) Get(id int64) (Task, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM tasks WHERE id=?`, id)
	var t Task
	var done int
	err := row.Scan(&t.ID, &t.Kind, &t.Title, &t.DueDate, &t.DueTime, &t.Category, &t.Note, &done, &t.DoneAt, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	t.Done = done != 0
	return t, nil
}

// Add inserisce una nuova attività.
func (s *Store) Add(t Task) (Task, error) {
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	t.CreatedAt = time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO tasks(kind,title,due_date,due_time,category,note,done,done_at,created_at) VALUES(?,?,?,?,?,?,0,'',?)`,
		t.Kind, t.Title, t.DueDate, t.DueTime, t.Category, t.Note, t.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, err
	}
	t.ID = id
	return t, nil
}

// Update aggiorna i campi modificabili di una attività esistente.
func (s *Store) Update(t Task) (Task, error) {
	if t.ID <= 0 {
		return Task{}, ErrNotFound
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	res, err := s.db.Exec(
		`UPDATE tasks SET kind=?, title=?, due_date=?, due_time=?, category=?, note=? WHERE id=?`,
		t.Kind, t.Title, t.DueDate, t.DueTime, t.Category, t.Note, t.ID)
	if err != nil {
		return Task{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Task{}, ErrNotFound
	}
	return s.Get(t.ID)
}

// SetDone segna una attività come completata o la riporta tra quelle da fare.
func (s *Store) SetDone(id int64, done bool) error {
	d, at := 0, ""
	if done {
		d, at = 1, time.Now().Format(time.RFC3339)
	}
	res, err := s.db.Exec(`UPDATE tasks SET done=?, done_at=? WHERE id=?`, d, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete elimina definitivamente una attività.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCompleted svuota l'elenco delle attività completate.
func (s *Store) DeleteCompleted() error {
	_, err := s.db.Exec(`DELETE FROM tasks WHERE done=1`)
	return err
}

// Count restituisce il numero di attività da fare.
func (s *Store) Count(done bool) (int, error) {
	d := 0
	if done {
		d = 1
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE done=?`, d).Scan(&n)
	return n, err
}
