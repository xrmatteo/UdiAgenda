package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version dell'applicazione, mostrata nelle impostazioni.
const Version = "1.5.0"

// State è la fotografia completa dei dati inviata all'interfaccia
// dopo ogni operazione: l'interfaccia si limita a disegnarla.
type State struct {
	Today      string `json:"today"`
	TodayLabel string `json:"todayLabel"`
	Tasks      []View `json:"tasks"`
	Completed  []View `json:"completed"`
	// registro materie
	Subjects     []SubjectView `json:"subjects"`
	Average      float64       `json:"average"`
	AverageLabel string        `json:"averageLabel"`
	AverageClass string        `json:"averageClass"`
	Settings     Settings      `json:"settings"`
	Counts       Counts        `json:"counts"`
	DataDir      string        `json:"dataDir"`
	Version      string        `json:"version"`
	SystemDark   bool          `json:"systemDark"`
}

// Counts sono i contatori mostrati nell'intestazione del pannello.
type Counts struct {
	Todo      int `json:"todo"`
	Overdue   int `json:"overdue"`
	Today     int `json:"today"`
	Completed int `json:"completed"`
	Compiti   int `json:"compiti"`
	Verifiche int `json:"verifiche"`
	Subjects  int `json:"subjects"`
	Grades    int `json:"grades"`
}

// Service espone le operazioni dell'applicazione in modo indipendente
// dall'interfaccia (usato sia dal pannello WebView2 sia dai test).
type Service struct {
	mu       sync.Mutex
	store    *Store
	settings Settings
	setPath  string

	// OnSettingsChanged viene richiamato dopo il salvataggio delle impostazioni
	// (usato dal livello Windows per avvio automatico, tema, posizione linguetta).
	OnSettingsChanged func(Settings)
	// OnClosePanel chiude il pannello laterale (impostato dal livello Windows).
	OnClosePanel func()
	// OnQuit chiude l'applicazione (impostato dal livello Windows).
	OnQuit func()
	// SystemDark indica se Windows sta usando il tema scuro (tema "automatico").
	SystemDark func() bool
	// OnModal segnala che è aperta una scheda (nuova attività / impostazioni):
	// mentre è aperta il pannello non si chiude da solo, per non perdere dati.
	OnModal func(open bool)
}

// NewService crea il servizio a partire da un database già aperto.
func NewService(store *Store, settingsPath string) *Service {
	return &Service{
		store:    store,
		settings: LoadSettings(settingsPath),
		setPath:  settingsPath,
	}
}

// Settings restituisce le impostazioni correnti.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// State costruisce lo stato corrente calcolando priorità ed etichette.
func (s *Service) State() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *Service) stateLocked() (State, error) {
	today := Today()
	todo, err := s.store.List(false)
	if err != nil {
		return State{}, err
	}
	st := State{
		Today:      today.Format(DateLayout),
		TodayLabel: FormatFullDate(today),
		Tasks:      make([]View, 0, len(todo)),
		Completed:  []View{},
		Settings:   s.settings,
		DataDir:    DataDir(),
		Version:    Version,
	}
	if s.SystemDark != nil {
		st.SystemDark = s.SystemDark()
	}
	for _, t := range todo {
		v := NewView(t, today)
		st.Tasks = append(st.Tasks, v)
		switch v.Level {
		case LevelOverdue:
			st.Counts.Overdue++
		case LevelToday:
			st.Counts.Today++
		}
		if v.Kind == KindVerifica {
			st.Counts.Verifiche++
		} else {
			st.Counts.Compiti++
		}
	}
	// ordinamento definitivo: scadenza più vicina per prima
	sort.SliceStable(st.Tasks, func(i, j int) bool {
		a, b := st.Tasks[i], st.Tasks[j]
		if a.DueDate != b.DueDate {
			return a.DueDate < b.DueDate
		}
		if (a.DueTime == "") != (b.DueTime == "") {
			return a.DueTime != ""
		}
		if a.DueTime != b.DueTime {
			return a.DueTime < b.DueTime
		}
		return a.ID < b.ID
	})
	st.Counts.Todo = len(st.Tasks)

	doneCount, err := s.store.Count(true)
	if err != nil {
		return State{}, err
	}
	st.Counts.Completed = doneCount
	if s.settings.ShowCompleted && doneCount > 0 {
		done, err := s.store.List(true)
		if err != nil {
			return State{}, err
		}
		// i completati più recenti per primi
		sort.SliceStable(done, func(i, j int) bool { return done[i].DoneAt > done[j].DoneAt })
		if len(done) > 50 {
			done = done[:50]
		}
		for _, t := range done {
			st.Completed = append(st.Completed, NewView(t, today))
		}
	}
	// registro materie: medie calcolate a ogni lettura
	subjects, err := s.store.ListSubjects()
	if err != nil {
		return State{}, err
	}
	grades, err := s.store.ListGrades()
	if err != nil {
		return State{}, err
	}
	st.Subjects, st.Average, st.AverageLabel = BuildSubjectViews(subjects, grades)
	st.Counts.Subjects = len(subjects)
	st.Counts.Grades = len(grades)
	st.AverageClass = GradeClass(st.Average)
	if len(grades) == 0 {
		st.AverageClass = "none"
	}

	return st, nil
}

type taskPayload struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	DueDate  string `json:"dueDate"`
	DueTime  string `json:"dueTime"`
	Category string `json:"category"`
	Note     string `json:"note"`
}

type idPayload struct {
	ID   int64 `json:"id"`
	Done bool  `json:"done"`
}

type subjectPayload struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Teacher string `json:"teacher"`
}

type gradePayload struct {
	ID        int64   `json:"id"`
	SubjectID int64   `json:"subjectId"`
	Subject   string  `json:"subject"`
	Value     float64 `json:"value"`
	Date      string  `json:"date"`
	Type      string  `json:"type"`
	Note      string  `json:"note"`
}

// resolveSubject trova la materia indicata per id oppure per nome,
// creandola al volo se l'utente ne ha scritta una nuova.
func (s *Service) resolveSubject(p gradePayload) (int64, error) {
	if p.SubjectID > 0 {
		return p.SubjectID, nil
	}
	m, err := s.store.EnsureSubject(p.Subject)
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

// Call è il punto di ingresso unico usato dal ponte JavaScript.
// Ogni operazione restituisce lo stato aggiornato, così l'interfaccia
// non deve mantenere logica propria.
func (s *Service) Call(method string, raw json.RawMessage) (any, error) {
	switch strings.TrimSpace(method) {
	case "state":
		return s.State()

	case "add":
		var p taskPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, err := s.store.Add(Task{
			Kind: p.Kind, Title: p.Title, DueDate: p.DueDate, DueTime: p.DueTime,
			Category: p.Category, Note: p.Note,
		}); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "update":
		var p taskPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, err := s.store.Update(Task{
			ID: p.ID, Kind: p.Kind, Title: p.Title, DueDate: p.DueDate, DueTime: p.DueTime,
			Category: p.Category, Note: p.Note,
		}); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "setDone":
		var p idPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.SetDone(p.ID, p.Done); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "delete":
		var p idPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.Delete(p.ID); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "clearCompleted":
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.DeleteCompleted(); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "saveSettings":
		var p Settings
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		p.Normalize()
		s.mu.Lock()
		s.settings = p
		err := SaveSettings(s.setPath, p)
		cb := s.OnSettingsChanged
		st, err2 := s.stateLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("impossibile salvare le impostazioni: %w", err)
		}
		if cb != nil {
			cb(p)
		}
		return st, err2

	case "addSubject":
		var p subjectPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, err := s.store.EnsureSubject(p.Name); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "updateSubject", "renameSubject":
		var p subjectPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.UpdateSubject(p.ID, p.Name, p.Teacher); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "deleteSubject":
		var p subjectPayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.DeleteSubject(p.ID); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "addGrade":
		var p gradePayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		id, err := s.resolveSubject(p)
		if err != nil {
			return nil, err
		}
		if _, err := s.store.AddGrade(Grade{
			SubjectID: id, Value: p.Value, Date: p.Date, Type: p.Type, Note: p.Note,
		}); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "updateGrade":
		var p gradePayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		id, err := s.resolveSubject(p)
		if err != nil {
			return nil, err
		}
		if err := s.store.UpdateGrade(Grade{
			ID: p.ID, SubjectID: id, Value: p.Value, Date: p.Date, Type: p.Type, Note: p.Note,
		}); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "deleteGrade":
		var p gradePayload
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.DeleteGrade(p.ID); err != nil {
			return nil, err
		}
		return s.stateLocked()

	case "setModal":
		var p struct {
			Open bool `json:"open"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if s.OnModal != nil {
			s.OnModal(p.Open)
		}
		return true, nil

	case "closePanel":
		if s.OnClosePanel != nil {
			s.OnClosePanel()
		}
		return true, nil

	case "quit":
		if s.OnQuit != nil {
			s.OnQuit()
		}
		return true, nil

	case "ping":
		return time.Now().Format(time.RFC3339), nil
	}
	return nil, errors.New("metodo sconosciuto: " + method)
}

func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return errors.New("parametri mancanti")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("parametri non validi")
	}
	return nil
}
