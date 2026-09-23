package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DateLayout è il formato usato per salvare le date nel database.
const DateLayout = "2006-01-02"

// TimeLayout è il formato usato per salvare l'ora (opzionale).
const TimeLayout = "15:04"

// Tipi di attività: le due agende del pannello.
const (
	KindCompito  = "compito"
	KindVerifica = "verifica"
)

// KindTitle è il nome della sezione a cui appartiene l'attività.
func KindTitle(kind string) string {
	if kind == KindVerifica {
		return "Verifiche"
	}
	return "Compiti"
}

// Task è una attività: compito, verifica, progetto, appuntamento, scadenza.
type Task struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // compito | verifica
	Title     string `json:"title"`
	DueDate   string `json:"dueDate"`  // sempre valorizzata, formato 2006-01-02
	DueTime   string `json:"dueTime"`  // "" oppure 15:04
	Category  string `json:"category"` // opzionale
	Note      string `json:"note"`     // opzionale
	Done      bool   `json:"done"`
	DoneAt    string `json:"doneAt"`
	CreatedAt string `json:"createdAt"`
}

// Level è il livello di urgenza calcolato automaticamente dalla data.
type Level string

const (
	LevelOverdue  Level = "overdue"  // scaduto
	LevelToday    Level = "today"    // oggi
	LevelTomorrow Level = "tomorrow" // domani
	LevelSoon     Level = "soon"     // entro 7 giorni
	LevelLater    Level = "later"    // oltre 7 giorni
)

// Order restituisce l'ordine di gravità del livello (0 = più urgente).
func (l Level) Order() int {
	switch l {
	case LevelOverdue:
		return 0
	case LevelToday:
		return 1
	case LevelTomorrow:
		return 2
	case LevelSoon:
		return 3
	default:
		return 4
	}
}

// GroupTitle è l'intestazione del gruppo mostrata nel pannello.
func (l Level) GroupTitle() string {
	switch l {
	case LevelOverdue:
		return "SCADUTO"
	case LevelToday:
		return "OGGI"
	case LevelTomorrow:
		return "DOMANI"
	case LevelSoon:
		return "PROSSIMI 7 GIORNI"
	default:
		return "PIÙ AVANTI"
	}
}

var mesiBrevi = [...]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"}
var giorniBrevi = [...]string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"}

// ParseDate interpreta una data nel formato del database.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(DateLayout, strings.TrimSpace(s), time.Local)
}

// Today restituisce la data odierna azzerata all'orario 00:00 locale.
func Today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
}

// DaysUntil restituisce i giorni di calendario che mancano alla scadenza
// rispetto al giorno di riferimento (negativi se la data è passata).
func DaysUntil(due, today time.Time) int {
	d := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.Local)
	t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	return int(d.Sub(t).Hours() / 24)
}

// LevelForDays applica la regola di priorità automatica richiesta.
func LevelForDays(days int) Level {
	switch {
	case days < 0:
		return LevelOverdue
	case days == 0:
		return LevelToday
	case days == 1:
		return LevelTomorrow
	case days <= 7:
		return LevelSoon
	default:
		return LevelLater
	}
}

// BadgeForDays è l'etichetta breve mostrata sopra ogni attività.
func BadgeForDays(days int, due time.Time) string {
	switch {
	case days < -1:
		return fmt.Sprintf("SCADUTO DA %d GIORNI", -days)
	case days == -1:
		return "SCADUTO IERI"
	case days == 0:
		return "OGGI"
	case days == 1:
		return "DOMANI"
	case days <= 14:
		return fmt.Sprintf("TRA %d GIORNI", days)
	default:
		return strings.ToUpper(FormatShortDate(due))
	}
}

// FormatShortDate produce ad esempio "mer 3 set".
func FormatShortDate(t time.Time) string {
	return fmt.Sprintf("%s %d %s", giorniBrevi[int(t.Weekday())], t.Day(), mesiBrevi[int(t.Month())-1])
}

// FormatLongDate produce ad esempio "mercoledì 3 settembre 2026" in forma breve.
func FormatFullDate(t time.Time) string {
	return fmt.Sprintf("%s %d %s %d", giorniBrevi[int(t.Weekday())], t.Day(), mesiBrevi[int(t.Month())-1], t.Year())
}

// Validate normalizza e verifica i campi obbligatori di una attività.
func (t *Task) Validate() error {
	if t.Kind != KindVerifica {
		t.Kind = KindCompito
	}
	t.Title = strings.TrimSpace(t.Title)
	t.DueDate = strings.TrimSpace(t.DueDate)
	t.DueTime = strings.TrimSpace(t.DueTime)
	t.Category = strings.TrimSpace(t.Category)
	t.Note = strings.TrimSpace(t.Note)

	if t.Title == "" {
		return errors.New("il nome dell'attività è obbligatorio")
	}
	if len([]rune(t.Title)) > 200 {
		t.Title = string([]rune(t.Title)[:200])
	}
	if len([]rune(t.Category)) > 60 {
		t.Category = string([]rune(t.Category)[:60])
	}
	if len([]rune(t.Note)) > 2000 {
		t.Note = string([]rune(t.Note)[:2000])
	}
	if t.DueDate == "" {
		return errors.New("la data di scadenza è obbligatoria")
	}
	if _, err := ParseDate(t.DueDate); err != nil {
		return errors.New("data non valida (formato richiesto: AAAA-MM-GG)")
	}
	if t.DueTime != "" {
		if _, err := time.Parse(TimeLayout, t.DueTime); err != nil {
			return errors.New("ora non valida (formato richiesto: HH:MM)")
		}
	}
	return nil
}

// View è la versione dell'attività arricchita con i dati calcolati
// che servono all'interfaccia.
type View struct {
	Task
	Level      Level  `json:"level"`
	LevelOrder int    `json:"levelOrder"`
	GroupTitle string `json:"groupTitle"`
	Badge      string `json:"badge"`
	Days       int    `json:"days"`
	DateLabel  string `json:"dateLabel"`
}

// NewView calcola priorità ed etichette di una attività rispetto a "today".
func NewView(t Task, today time.Time) View {
	v := View{Task: t}
	due, err := ParseDate(t.DueDate)
	if err != nil {
		due = today
	}
	v.Days = DaysUntil(due, today)
	v.Level = LevelForDays(v.Days)
	v.LevelOrder = v.Level.Order()
	v.GroupTitle = v.Level.GroupTitle()
	v.Badge = BadgeForDays(v.Days, due)
	v.DateLabel = FormatShortDate(due)
	if t.DueTime != "" {
		v.DateLabel += " · " + t.DueTime
	}
	return v
}
