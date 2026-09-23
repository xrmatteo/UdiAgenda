package core

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Scala scolastica svizzera: i voti vanno da 1 a 6, la sufficienza è 4.
const (
	GradeMin  = 1.0
	GradeMax  = 6.0
	GradePass = 4.0
)

// Subject è una materia del registro (Matematica, Inglese, ...).
type Subject struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Teacher   string `json:"teacher"`
	CreatedAt string `json:"createdAt"`
}

// Grade è un voto preso in una materia.
type Grade struct {
	ID        int64   `json:"id"`
	SubjectID int64   `json:"subjectId"`
	Value     float64 `json:"value"`
	Date      string  `json:"date"`
	Type      string  `json:"type"` // scritto | orale | pratico | ""
	Note      string  `json:"note"`
	CreatedAt string  `json:"createdAt"`
}

// Tipi di prova ammessi (facoltativi).
var GradeTypes = []string{"scritto", "orale", "pratico"}

// ErrSubjectNotFound viene restituito quando la materia non esiste.
var ErrSubjectNotFound = errors.New("materia non trovata")

// ErrGradeNotFound viene restituito quando il voto non esiste.
var ErrGradeNotFound = errors.New("voto non trovato")

// Validate normalizza e verifica un voto.
func (g *Grade) Validate() error {
	g.Date = strings.TrimSpace(g.Date)
	g.Note = strings.TrimSpace(g.Note)
	g.Type = strings.ToLower(strings.TrimSpace(g.Type))
	ok := g.Type == ""
	for _, t := range GradeTypes {
		if g.Type == t {
			ok = true
		}
	}
	if !ok {
		g.Type = ""
	}
	if len([]rune(g.Note)) > 500 {
		g.Note = string([]rune(g.Note)[:500])
	}
	if g.SubjectID <= 0 {
		return errors.New("la materia è obbligatoria")
	}
	if math.IsNaN(g.Value) || g.Value < GradeMin || g.Value > GradeMax {
		return errors.New("il voto deve essere compreso tra 1 e 6")
	}
	g.Value = math.Round(g.Value*100) / 100
	if g.Date == "" {
		g.Date = Today().Format(DateLayout)
	}
	if _, err := ParseDate(g.Date); err != nil {
		return errors.New("data del voto non valida")
	}
	return nil
}

// NormalizeSubjectName ripulisce il nome di una materia.
func NormalizeSubjectName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return name
}

// NormalizeTeacher ripulisce il nome del docente (facoltativo).
func NormalizeTeacher(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return name
}

// ---------------------------------------------------------------- materie

// ListSubjects restituisce le materie in ordine alfabetico.
func (s *Store) ListSubjects() ([]Subject, error) {
	rows, err := s.db.Query(`SELECT id,name,teacher,created_at FROM subjects ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subject{}
	for rows.Next() {
		var m Subject
		if err := rows.Scan(&m.ID, &m.Name, &m.Teacher, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SubjectByName cerca una materia per nome (senza distinzione fra maiuscole e minuscole).
func (s *Store) SubjectByName(name string) (Subject, error) {
	name = NormalizeSubjectName(name)
	row := s.db.QueryRow(`SELECT id,name,teacher,created_at FROM subjects WHERE name=? COLLATE NOCASE`, name)
	var m Subject
	err := row.Scan(&m.ID, &m.Name, &m.Teacher, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subject{}, ErrSubjectNotFound
	}
	return m, err
}

// EnsureSubject restituisce la materia con quel nome, creandola se non esiste.
func (s *Store) EnsureSubject(name string) (Subject, error) {
	name = NormalizeSubjectName(name)
	if name == "" {
		return Subject{}, errors.New("il nome della materia è obbligatorio")
	}
	if m, err := s.SubjectByName(name); err == nil {
		return m, nil
	} else if !errors.Is(err, ErrSubjectNotFound) {
		return Subject{}, err
	}
	now := time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(`INSERT INTO subjects(name,created_at) VALUES(?,?)`, name, now)
	if err != nil {
		return Subject{}, fmt.Errorf("impossibile creare la materia: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Subject{}, err
	}
	return Subject{ID: id, Name: name, CreatedAt: now}, nil
}

// UpdateSubject cambia nome e docente di una materia.
func (s *Store) UpdateSubject(id int64, name, teacher string) error {
	name = NormalizeSubjectName(name)
	teacher = NormalizeTeacher(teacher)
	if name == "" {
		return errors.New("il nome della materia è obbligatorio")
	}
	if other, err := s.SubjectByName(name); err == nil && other.ID != id {
		return errors.New("esiste già una materia con questo nome")
	}
	res, err := s.db.Exec(`UPDATE subjects SET name=?, teacher=? WHERE id=?`, name, teacher, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSubjectNotFound
	}
	return nil
}

// DeleteSubject elimina una materia e tutti i suoi voti.
func (s *Store) DeleteSubject(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM grades WHERE subject_id=?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM subjects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSubjectNotFound
	}
	return nil
}

// ---------------------------------------------------------------- voti

// ListGrades restituisce tutti i voti, dal più recente al più vecchio.
func (s *Store) ListGrades() ([]Grade, error) {
	rows, err := s.db.Query(
		`SELECT id,subject_id,value,date,type,note,created_at FROM grades ORDER BY date DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grade{}
	for rows.Next() {
		var g Grade
		if err := rows.Scan(&g.ID, &g.SubjectID, &g.Value, &g.Date, &g.Type, &g.Note, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddGrade inserisce un voto.
func (s *Store) AddGrade(g Grade) (Grade, error) {
	if err := g.Validate(); err != nil {
		return Grade{}, err
	}
	g.CreatedAt = time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO grades(subject_id,value,date,type,note,created_at) VALUES(?,?,?,?,?,?)`,
		g.SubjectID, g.Value, g.Date, g.Type, g.Note, g.CreatedAt)
	if err != nil {
		return Grade{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Grade{}, err
	}
	g.ID = id
	return g, nil
}

// UpdateGrade aggiorna un voto esistente.
func (s *Store) UpdateGrade(g Grade) error {
	if g.ID <= 0 {
		return ErrGradeNotFound
	}
	if err := g.Validate(); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE grades SET subject_id=?, value=?, date=?, type=?, note=? WHERE id=?`,
		g.SubjectID, g.Value, g.Date, g.Type, g.Note, g.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGradeNotFound
	}
	return nil
}

// DeleteGrade elimina un voto.
func (s *Store) DeleteGrade(id int64) error {
	res, err := s.db.Exec(`DELETE FROM grades WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGradeNotFound
	}
	return nil
}

// ---------------------------------------------------------------- viste

// GradeView è un voto con le etichette pronte per l'interfaccia.
type GradeView struct {
	Grade
	DateLabel string `json:"dateLabel"`
	Label     string `json:"label"` // il voto formattato: 7, 7.5, 8.25
	Class     string `json:"class"` // fascia di colore
}

// SubjectView è una materia con i suoi voti e la media.
type SubjectView struct {
	ID           int64       `json:"id"`
	Name         string      `json:"name"`
	Teacher      string      `json:"teacher"`
	Count        int         `json:"count"`
	Average      float64     `json:"average"`
	AverageLabel string      `json:"averageLabel"`
	Class        string      `json:"class"`
	Grades       []GradeView `json:"grades"`
}

// GradeClass restituisce la fascia di colore di un voto (scala svizzera):
// sotto il 4 rosso, sotto il 4.5 arancione, sotto il 5 giallo, dal 5 verde.
func GradeClass(v float64) string {
	switch {
	case v <= 0:
		return "none"
	case v < GradePass:
		return "bad"
	case v < 4.5:
		return "low"
	case v < 5:
		return "mid"
	default:
		return "good"
	}
}

// FormatGrade scrive un voto senza zeri inutili (5, 4.5, 4.75).
func FormatGrade(v float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
	if s == "" {
		s = "0"
	}
	return s
}

// FormatAverage scrive una media sempre con un decimale (5.2, 4.9, 6.0),
// come sulle pagelle svizzere. L'arrotondamento è per eccesso a metà
// (5.25 diventa 5.3), non quello statistico usato da Go.
func FormatAverage(v float64) string {
	return fmt.Sprintf("%.1f", math.Floor(v*10+0.5+1e-6)/10)
}

// NewGradeView prepara un voto per l'interfaccia.
func NewGradeView(g Grade) GradeView {
	v := GradeView{Grade: g, Label: FormatGrade(g.Value), Class: GradeClass(g.Value)}
	if d, err := ParseDate(g.Date); err == nil {
		v.DateLabel = FormatShortDate(d)
	} else {
		v.DateLabel = g.Date
	}
	return v
}

// BuildSubjectViews unisce materie e voti calcolando le medie.
// Le materie sono ordinate per nome; i voti dal più recente.
func BuildSubjectViews(subjects []Subject, grades []Grade) ([]SubjectView, float64, string) {
	byID := map[int64]*SubjectView{}
	out := make([]SubjectView, 0, len(subjects))
	for _, m := range subjects {
		out = append(out, SubjectView{ID: m.ID, Name: m.Name, Teacher: m.Teacher, Grades: []GradeView{}})
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}

	sum, n := 0.0, 0
	for _, g := range grades {
		sv := byID[g.SubjectID]
		if sv == nil {
			continue // voto orfano: ignorato
		}
		sv.Grades = append(sv.Grades, NewGradeView(g))
		sv.Average += g.Value
		sv.Count++
		sum += g.Value
		n++
	}
	for i := range out {
		sv := &out[i]
		sort.SliceStable(sv.Grades, func(a, b int) bool {
			if sv.Grades[a].Date != sv.Grades[b].Date {
				return sv.Grades[a].Date > sv.Grades[b].Date
			}
			return sv.Grades[a].ID > sv.Grades[b].ID
		})
		if sv.Count > 0 {
			sv.Average = math.Round(sv.Average/float64(sv.Count)*100) / 100
			sv.AverageLabel = FormatAverage(sv.Average)
			sv.Class = GradeClass(sv.Average)
		} else {
			sv.Average = 0
			sv.AverageLabel = ""
			sv.Class = "none"
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})

	if n == 0 {
		return out, 0, ""
	}
	general := math.Round(sum/float64(n)*100) / 100
	return out, general, FormatAverage(general)
}
