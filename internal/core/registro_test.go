package core

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// Crea un database con lo schema della versione 1 (senza colonna kind e
// senza registro) e ci mette dentro dei dati, come farebbe la versione
// dell'applicazione già installata sul PC.
func creaDatabaseVecchio(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("apertura: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE tasks (
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
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta(key,value) VALUES('schema_version','1');
`)
	if err != nil {
		t.Fatalf("schema v1: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO tasks(title,due_date,due_time,category,note,done,done_at,created_at)
		 VALUES('Compito vecchio','2026-09-10','','Storia','pagine 20-30',0,'','2026-09-01T10:00:00Z'),
		        ('Roba fatta','2026-08-01','','','',1,'2026-08-01T12:00:00Z','2026-07-30T10:00:00Z')`)
	if err != nil {
		t.Fatalf("dati v1: %v", err)
	}
}

func TestMigrazioneDaDatabaseVecchio(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "udiagenda.db")
	creaDatabaseVecchio(t, dbPath)

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("la migrazione ha fallito l'apertura: %v", err)
	}
	defer store.Close()

	tasks, err := store.List(false)
	if err != nil {
		t.Fatalf("lettura: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Compito vecchio" {
		t.Fatalf("i dati esistenti sono andati persi: %+v", tasks)
	}
	if tasks[0].Kind != KindCompito {
		t.Fatalf("le attività già presenti devono diventare compiti, trovato %q", tasks[0].Kind)
	}
	if tasks[0].Category != "Storia" || tasks[0].Note != "pagine 20-30" {
		t.Fatalf("campi persi nella migrazione: %+v", tasks[0])
	}
	done, err := store.Count(true)
	if err != nil || done != 1 {
		t.Fatalf("le attività completate devono restare: %d (%v)", done, err)
	}

	// il registro nuovo deve essere utilizzabile subito
	m, err := store.EnsureSubject("Matematica")
	if err != nil {
		t.Fatalf("creazione materia dopo migrazione: %v", err)
	}
	if _, err := store.AddGrade(Grade{SubjectID: m.ID, Value: 5.5, Date: "2026-09-01"}); err != nil {
		t.Fatalf("inserimento voto dopo migrazione: %v", err)
	}

	// riaprire una seconda volta non deve rompere nulla (migrazione idempotente)
	store.Close()
	store2, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("seconda apertura: %v", err)
	}
	defer store2.Close()
	tasks2, _ := store2.List(false)
	if len(tasks2) != 1 {
		t.Fatalf("dati persi alla riapertura: %+v", tasks2)
	}
	grades, _ := store2.ListGrades()
	if len(grades) != 1 || grades[0].Value != 5.5 {
		t.Fatalf("voto non persistito: %+v", grades)
	}
}

func TestCompitiEVerificheSeparati(t *testing.T) {
	svc, _ := openTemp(t)
	call(t, svc, "add", map[string]any{"kind": "compito", "title": "Esercizi di algebra", "dueDate": day(2)})
	call(t, svc, "add", map[string]any{"kind": "verifica", "title": "Verifica di storia", "dueDate": day(4)})
	st := call(t, svc, "add", map[string]any{"title": "Senza tipo", "dueDate": day(6)})

	if st.Counts.Compiti != 2 || st.Counts.Verifiche != 1 {
		t.Fatalf("conteggi per sezione errati: %+v", st.Counts)
	}
	kinds := map[string]string{}
	for _, v := range st.Tasks {
		kinds[v.Title] = v.Kind
	}
	if kinds["Verifica di storia"] != KindVerifica {
		t.Fatalf("la verifica non è stata salvata come tale: %v", kinds)
	}
	if kinds["Senza tipo"] != KindCompito {
		t.Fatalf("senza tipo indicato deve valere compito: %v", kinds)
	}

	// spostare una attività da una agenda all'altra
	var id int64
	for _, v := range st.Tasks {
		if v.Title == "Esercizi di algebra" {
			id = v.ID
		}
	}
	st = call(t, svc, "update", map[string]any{
		"id": id, "kind": "verifica", "title": "Esercizi di algebra", "dueDate": day(2)})
	if st.Counts.Compiti != 1 || st.Counts.Verifiche != 2 {
		t.Fatalf("spostamento fra agende non riuscito: %+v", st.Counts)
	}
}

func TestVotiMedieEFasce(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "addGrade", map[string]any{"subject": "Matematica", "value": 5.5, "date": "2026-09-01"})
	if len(st.Subjects) != 1 || st.Subjects[0].Name != "Matematica" {
		t.Fatalf("materia non creata al volo: %+v", st.Subjects)
	}
	st = call(t, svc, "addGrade", map[string]any{"subject": "matematica", "value": 4.5, "date": "2026-09-05"})
	if len(st.Subjects) != 1 {
		t.Fatalf("la materia non deve essere duplicata per maiuscole diverse: %+v", st.Subjects)
	}
	st = call(t, svc, "addGrade", map[string]any{"subject": "Inglese", "value": 3.5, "date": "2026-09-03"})

	var mat, ing SubjectView
	for _, s := range st.Subjects {
		if s.Name == "Matematica" {
			mat = s
		}
		if s.Name == "Inglese" {
			ing = s
		}
	}
	if mat.Count != 2 || mat.Average != 5 || mat.AverageLabel != "5.0" {
		t.Fatalf("media della materia errata: %+v", mat)
	}
	if mat.Class != "good" || ing.Class != "bad" {
		t.Fatalf("fasce di colore errate: %s / %s", mat.Class, ing.Class)
	}
	if st.AverageLabel != "4.5" || st.Average != 4.5 {
		t.Fatalf("media generale errata: %v %q", st.Average, st.AverageLabel)
	}
	// i voti della materia sono ordinati dal più recente
	if len(mat.Grades) != 2 || mat.Grades[0].Date != "2026-09-05" {
		t.Fatalf("ordinamento voti errato: %+v", mat.Grades)
	}
	if mat.Grades[0].Label != "4.5" || mat.Grades[0].DateLabel == "" {
		t.Fatalf("etichette del voto errate: %+v", mat.Grades[0])
	}
	if st.Counts.Subjects != 2 || st.Counts.Grades != 3 {
		t.Fatalf("contatori registro errati: %+v", st.Counts)
	}
}

func TestVotoNonValido(t *testing.T) {
	svc, _ := openTemp(t)
	for _, bad := range []string{
		`{"subject":"Storia","value":0}`,
		`{"subject":"Storia","value":6.5}`,
		`{"subject":"Storia","value":11}`,
		`{"subject":"","value":5}`,
		`{"subject":"Storia","value":5,"date":"31/12/2026"}`,
	} {
		if _, err := svc.Call("addGrade", json.RawMessage(bad)); err == nil {
			t.Fatalf("questo voto doveva essere rifiutato: %s", bad)
		}
	}
	// senza data vale oggi
	st := call(t, svc, "addGrade", map[string]any{"subject": "Storia", "value": 4.75})
	if st.Subjects[0].Grades[0].Date != Today().Format(DateLayout) {
		t.Fatalf("la data predefinita deve essere oggi: %+v", st.Subjects[0].Grades[0])
	}
}

func TestModificaEliminaVotoEMateria(t *testing.T) {
	svc, _ := openTemp(t)
	st := call(t, svc, "addGrade", map[string]any{"subject": "Fisica", "value": 4, "type": "orale"})
	g := st.Subjects[0].Grades[0]
	subjID := st.Subjects[0].ID

	st = call(t, svc, "updateGrade", map[string]any{
		"id": g.ID, "subjectId": subjID, "value": 5.25, "date": g.Date, "type": "scritto", "note": "recupero"})
	got := st.Subjects[0].Grades[0]
	if got.Value != 5.25 || got.Type != "scritto" || got.Note != "recupero" {
		t.Fatalf("modifica del voto non applicata: %+v", got)
	}
	if st.Subjects[0].AverageLabel != "5.3" {
		t.Fatalf("media non ricalcolata: %q", st.Subjects[0].AverageLabel)
	}

	// rinomina materia
	st = call(t, svc, "updateSubject", map[string]any{"id": subjID, "name": "Fisica e laboratorio", "teacher": "Renata Foglia"})
	if st.Subjects[0].Teacher != "Renata Foglia" {
		t.Fatalf("docente non salvato: %+v", st.Subjects[0])
	}
	if st.Subjects[0].Name != "Fisica e laboratorio" {
		t.Fatalf("rinomina non applicata: %+v", st.Subjects[0])
	}
	call(t, svc, "addSubject", map[string]any{"name": "Chimica"})
	if _, err := svc.Call("updateSubject", json.RawMessage(
		`{"id":`+itoa(subjID)+`,"name":"Chimica"}`)); err == nil {
		t.Fatal("due materie con lo stesso nome non sono ammesse")
	}

	// elimina voto
	st = call(t, svc, "deleteGrade", map[string]any{"id": got.ID})
	if st.Counts.Grades != 0 || st.Subjects[0].AverageLabel != "" {
		t.Fatalf("voto non eliminato: %+v", st.Counts)
	}

	// eliminare la materia porta via anche i suoi voti
	call(t, svc, "addGrade", map[string]any{"subjectId": subjID, "value": 4})
	st = call(t, svc, "deleteSubject", map[string]any{"id": subjID})
	if st.Counts.Subjects != 1 || st.Counts.Grades != 0 {
		t.Fatalf("eliminazione materia incompleta: %+v", st.Counts)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
