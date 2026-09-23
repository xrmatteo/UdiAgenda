package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "udiagenda.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("apertura database: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	svc := NewService(store, filepath.Join(dir, "settings.json"))
	return svc, dbPath
}

func day(offset int) string {
	return Today().AddDate(0, 0, offset).Format(DateLayout)
}

func call(t *testing.T, svc *Service, method string, params any) State {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		raw = b
	}
	res, err := svc.Call(method, raw)
	if err != nil {
		t.Fatalf("chiamata %s fallita: %v", method, err)
	}
	st, ok := res.(State)
	if !ok {
		t.Fatalf("chiamata %s: tipo di ritorno inatteso %T", method, res)
	}
	return st
}

func TestDatabaseVieneCreato(t *testing.T) {
	_, dbPath := openTemp(t)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("il file del database non è stato creato: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("la cartella dati non è stata creata: %v", err)
	}
}

func TestCreazioneAttivitaMinima(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "add", map[string]any{"title": "  Compito matematica  ", "dueDate": day(1)})
	if len(st.Tasks) != 1 {
		t.Fatalf("attese 1 attività, trovate %d", len(st.Tasks))
	}
	if st.Tasks[0].Title != "Compito matematica" {
		t.Fatalf("titolo non normalizzato: %q", st.Tasks[0].Title)
	}
	if st.Tasks[0].Level != LevelTomorrow || st.Tasks[0].Badge != "DOMANI" {
		t.Fatalf("priorità automatica errata: %v / %v", st.Tasks[0].Level, st.Tasks[0].Badge)
	}
}

func TestCampiObbligatori(t *testing.T) {
	svc, _ := openTemp(t)
	if _, err := svc.Call("add", json.RawMessage(`{"title":"","dueDate":"`+day(0)+`"}`)); err == nil {
		t.Fatal("un titolo vuoto doveva essere rifiutato")
	}
	if _, err := svc.Call("add", json.RawMessage(`{"title":"Verifica","dueDate":""}`)); err == nil {
		t.Fatal("una data vuota doveva essere rifiutata")
	}
	if _, err := svc.Call("add", json.RawMessage(`{"title":"Verifica","dueDate":"03/09/2026"}`)); err == nil {
		t.Fatal("una data nel formato sbagliato doveva essere rifiutata")
	}
	if _, err := svc.Call("add", json.RawMessage(`{"title":"Verifica","dueDate":"`+day(0)+`","dueTime":"25:99"}`)); err == nil {
		t.Fatal("un orario non valido doveva essere rifiutato")
	}
}

func TestOrdinamentoPerScadenza(t *testing.T) {
	svc, _ := openTemp(t)
	call(t, svc, "add", map[string]any{"title": "Verifica inglese", "dueDate": day(10)})
	call(t, svc, "add", map[string]any{"title": "Compito matematica", "dueDate": day(1)})
	call(t, svc, "add", map[string]any{"title": "Progetto Java", "dueDate": day(3)})
	call(t, svc, "add", map[string]any{"title": "Consegna arretrata", "dueDate": day(-2)})
	st := call(t, svc, "add", map[string]any{"title": "Colloquio", "dueDate": day(3), "dueTime": "09:30"})

	want := []string{"Consegna arretrata", "Compito matematica", "Colloquio", "Progetto Java", "Verifica inglese"}
	if len(st.Tasks) != len(want) {
		t.Fatalf("attese %d attività, trovate %d", len(want), len(st.Tasks))
	}
	for i, w := range want {
		if st.Tasks[i].Title != w {
			t.Fatalf("posizione %d: atteso %q, trovato %q", i, w, st.Tasks[i].Title)
		}
	}
	if st.Tasks[0].Level != LevelOverdue || st.Tasks[0].Badge != "SCADUTO DA 2 GIORNI" {
		t.Fatalf("attività scaduta non riconosciuta: %v %q", st.Tasks[0].Level, st.Tasks[0].Badge)
	}
	if st.Counts.Overdue != 1 || st.Counts.Todo != 5 {
		t.Fatalf("contatori errati: %+v", st.Counts)
	}
}

func TestLivelliDiUrgenza(t *testing.T) {
	cases := []struct {
		days  int
		level Level
		badge string
	}{
		{-5, LevelOverdue, "SCADUTO DA 5 GIORNI"},
		{-1, LevelOverdue, "SCADUTO IERI"},
		{0, LevelToday, "OGGI"},
		{1, LevelTomorrow, "DOMANI"},
		{3, LevelSoon, "TRA 3 GIORNI"},
		{7, LevelSoon, "TRA 7 GIORNI"},
		{8, LevelLater, "TRA 8 GIORNI"},
		{10, LevelLater, "TRA 10 GIORNI"},
	}
	today := Today()
	for _, c := range cases {
		v := NewView(Task{Title: "x", DueDate: today.AddDate(0, 0, c.days).Format(DateLayout)}, today)
		if v.Level != c.level {
			t.Fatalf("%+d giorni: atteso livello %s, trovato %s", c.days, c.level, v.Level)
		}
		if v.Badge != c.badge {
			t.Fatalf("%+d giorni: attesa etichetta %q, trovata %q", c.days, c.badge, v.Badge)
		}
		if v.Days != c.days {
			t.Fatalf("%+d giorni: conteggio errato %d", c.days, v.Days)
		}
	}
	// oltre 14 giorni viene mostrata la data
	v := NewView(Task{Title: "x", DueDate: today.AddDate(0, 0, 40).Format(DateLayout)}, today)
	if v.Level != LevelLater || len(v.Badge) < 5 {
		t.Fatalf("etichetta lontana errata: %q", v.Badge)
	}
}

func TestCambioGiornoRicalcolaPriorita(t *testing.T) {
	base := time.Date(2026, 9, 2, 0, 0, 0, 0, time.Local)
	task := Task{Title: "Compito", DueDate: "2026-09-03"}
	if v := NewView(task, base); v.Level != LevelTomorrow {
		t.Fatalf("il 2 settembre deve essere DOMANI, trovato %s", v.Level)
	}
	if v := NewView(task, base.AddDate(0, 0, 1)); v.Level != LevelToday {
		t.Fatalf("il 3 settembre deve essere OGGI, trovato %s", v.Level)
	}
	if v := NewView(task, base.AddDate(0, 0, 2)); v.Level != LevelOverdue {
		t.Fatalf("il 4 settembre deve essere SCADUTO, trovato %s", v.Level)
	}
}

func TestModificaAttivita(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "add", map[string]any{"title": "Compito", "dueDate": day(5)})
	id := st.Tasks[0].ID
	st = call(t, svc, "update", map[string]any{
		"id": id, "title": "Compito di storia", "dueDate": day(0),
		"dueTime": "08:15", "category": "Scuola", "note": "capitoli 3 e 4",
	})
	got := st.Tasks[0]
	if got.Title != "Compito di storia" || got.DueTime != "08:15" || got.Category != "Scuola" || got.Note != "capitoli 3 e 4" {
		t.Fatalf("modifica non applicata: %+v", got)
	}
	if got.Level != LevelToday {
		t.Fatalf("priorità non ricalcolata dopo la modifica: %s", got.Level)
	}
	if _, err := svc.Call("update", json.RawMessage(`{"id":99999,"title":"x","dueDate":"`+day(1)+`"}`)); err == nil {
		t.Fatal("la modifica di un id inesistente doveva fallire")
	}
}

func TestCompletamentoENascondimento(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "add", map[string]any{"title": "Compito", "dueDate": day(2)})
	call(t, svc, "add", map[string]any{"title": "Verifica", "dueDate": day(4)})
	id := st.Tasks[0].ID

	st = call(t, svc, "setDone", map[string]any{"id": id, "done": true})
	if len(st.Tasks) != 1 || st.Tasks[0].Title != "Verifica" {
		t.Fatalf("l'attività completata deve sparire dalla lista principale: %+v", st.Tasks)
	}
	if st.Counts.Completed != 1 {
		t.Fatalf("contatore completati errato: %+v", st.Counts)
	}
	if len(st.Completed) != 0 {
		t.Fatal("con ShowCompleted=false la sezione completati deve essere vuota")
	}

	// attivando l'opzione la sezione compare
	s := svc.Settings()
	s.ShowCompleted = true
	st = call(t, svc, "saveSettings", s)
	if len(st.Completed) != 1 || st.Completed[0].Title != "Compito" {
		t.Fatalf("sezione completati non popolata: %+v", st.Completed)
	}

	// ripristino
	st = call(t, svc, "setDone", map[string]any{"id": id, "done": false})
	if len(st.Tasks) != 2 {
		t.Fatalf("il ripristino non ha funzionato: %d", len(st.Tasks))
	}
}

func TestEliminazione(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "add", map[string]any{"title": "Da eliminare", "dueDate": day(1)})
	id := st.Tasks[0].ID
	st = call(t, svc, "delete", map[string]any{"id": id})
	if len(st.Tasks) != 0 {
		t.Fatalf("attività non eliminata: %+v", st.Tasks)
	}
	if _, err := svc.Call("delete", json.RawMessage(`{"id":4242}`)); err == nil {
		t.Fatal("l'eliminazione di un id inesistente doveva fallire")
	}
}

func TestPersistenzaDopoRiavvio(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "udiagenda.db")
	setPath := filepath.Join(dir, "settings.json")

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("apertura: %v", err)
	}
	svc := NewService(store, setPath)
	call(t, svc, "add", map[string]any{"title": "Progetto Java", "dueDate": day(3), "note": "consegna su classroom"})
	s := svc.Settings()
	s.Theme = "dark"
	s.PanelWidth = 460
	call(t, svc, "saveSettings", s)
	if err := store.Close(); err != nil {
		t.Fatalf("chiusura: %v", err)
	}

	// simulazione riavvio del programma / del PC
	store2, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("riapertura: %v", err)
	}
	defer store2.Close()
	svc2 := NewService(store2, setPath)
	st := call(t, svc2, "state", nil)
	if len(st.Tasks) != 1 || st.Tasks[0].Title != "Progetto Java" || st.Tasks[0].Note != "consegna su classroom" {
		t.Fatalf("dati non persistiti: %+v", st.Tasks)
	}
	if st.Settings.Theme != "dark" || st.Settings.PanelWidth != 460 {
		t.Fatalf("impostazioni non persistite: %+v", st.Settings)
	}
}

func TestImpostazioniValoriNonValidi(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "saveSettings", map[string]any{
		"theme": "fucsia", "tabSide": "sopra", "tabPosition": "boh", "panelWidth": 5000,
	})
	got := st.Settings
	if got.Theme != "auto" || got.TabSide != "right" || got.TabPosition != "center" || got.PanelWidth != 520 {
		t.Fatalf("valori non normalizzati: %+v", got)
	}
	def := DefaultSettings()
	if !def.StartWithWindows || def.Theme != "auto" || def.PanelWidth != 400 {
		t.Fatalf("default inattesi: %+v", def)
	}
}

func TestSvuotaCompletati(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "add", map[string]any{"title": "A", "dueDate": day(1)})
	call(t, svc, "setDone", map[string]any{"id": st.Tasks[0].ID, "done": true})
	st = call(t, svc, "clearCompleted", nil)
	if st.Counts.Completed != 0 {
		t.Fatalf("completati non svuotati: %+v", st.Counts)
	}
}

func TestMetodoSconosciuto(t *testing.T) {
	svc, _ := openTemp(t)
	if _, err := svc.Call("formatta_disco", nil); err == nil {
		t.Fatal("un metodo sconosciuto deve restituire errore")
	}
}
