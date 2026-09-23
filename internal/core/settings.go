package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings contiene le poche opzioni configurabili dell'applicazione.
type Settings struct {
	StartWithWindows bool   `json:"startWithWindows"`
	Theme            string `json:"theme"`         // auto | light | dark
	TabSide          string `json:"tabSide"`       // right | left
	TabPosition      string `json:"tabPosition"`   // top | center | bottom
	ShowCompleted    bool   `json:"showCompleted"` // mostra la sezione "Completati"
	PanelWidth       int    `json:"panelWidth"`    // 340..520
	Accent           string `json:"accent"`        // colore d'accento, vedi Accents
	HideTab          bool   `json:"hideTab"`       // linguetta a scomparsa quando il mouse è lontano
}

// Accent è un colore d'accento selezionabile dalle impostazioni.
// Light e Dark sono le due varianti (tema chiaro / tema scuro) e vengono
// usate sia dall'interfaccia sia dalla linguetta disegnata da Windows.
type Accent struct {
	Key   string
	Name  string
	Light [3]uint8
	Dark  [3]uint8
}

// Accents è la tavolozza disponibile nelle impostazioni.
var Accents = []Accent{
	{"blue", "Blu", [3]uint8{0x0F, 0x6C, 0xBD}, [3]uint8{0x4C, 0xC2, 0xFF}},
	{"teal", "Turchese", [3]uint8{0x0D, 0x7A, 0x72}, [3]uint8{0x4F, 0xD8, 0xCF}},
	{"green", "Verde", [3]uint8{0x2F, 0x7A, 0x3F}, [3]uint8{0x7A, 0xD4, 0x8A}},
	{"purple", "Viola", [3]uint8{0x71, 0x45, 0xAB}, [3]uint8{0xC4, 0xA2, 0xF5}},
	{"pink", "Rosa", [3]uint8{0xB3, 0x28, 0x6B}, [3]uint8{0xFF, 0x9D, 0xC8}},
	{"orange", "Arancio", [3]uint8{0xB3, 0x5C, 0x00}, [3]uint8{0xFF, 0xB2, 0x57}},
	{"red", "Rosso", [3]uint8{0xC0, 0x39, 0x2B}, [3]uint8{0xFF, 0x9C, 0x8F}},
	{"graphite", "Grafite", [3]uint8{0x4A, 0x4A, 0x55}, [3]uint8{0xB9, 0xB9, 0xC6}},
}

// AccentByKey restituisce il colore d'accento richiesto (o il blu di default).
func AccentByKey(key string) Accent {
	for _, a := range Accents {
		if a.Key == key {
			return a
		}
	}
	return Accents[0]
}

// DefaultSettings sono i valori usati al primo avvio.
func DefaultSettings() Settings {
	return Settings{
		StartWithWindows: true,
		Theme:            "auto",
		TabSide:          "right",
		TabPosition:      "center",
		ShowCompleted:    false,
		PanelWidth:       400,
		Accent:           "blue",
		HideTab:          true,
	}
}

// Normalize riporta eventuali valori non validi ai valori di default.
func (s *Settings) Normalize() {
	switch s.Theme {
	case "auto", "light", "dark":
	default:
		s.Theme = "auto"
	}
	switch s.TabSide {
	case "right", "left":
	default:
		s.TabSide = "right"
	}
	switch s.TabPosition {
	case "top", "center", "bottom":
	default:
		s.TabPosition = "center"
	}
	if s.PanelWidth < 340 {
		s.PanelWidth = 340
	}
	if s.PanelWidth > 520 {
		s.PanelWidth = 520
	}
	known := false
	for _, a := range Accents {
		if a.Key == s.Accent {
			known = true
			break
		}
	}
	if !known {
		s.Accent = Accents[0].Key
	}
}

// LoadSettings legge le impostazioni dal disco; se il file non esiste
// restituisce i valori di default (senza errore).
func LoadSettings(path string) Settings {
	s := DefaultSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings()
	}
	s.Normalize()
	return s
}

// SaveSettings scrive le impostazioni sul disco in modo atomico.
func SaveSettings(path string, s Settings) error {
	s.Normalize()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
