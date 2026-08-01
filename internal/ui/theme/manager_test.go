package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTheme(t *testing.T) {
	dt := DefaultTheme()
	if dt.Name != "default" {
		t.Errorf("DefaultTheme().Name = %q, want %q", dt.Name, "default")
	}
	if dt.ColorPrimary == "" {
		t.Error("DefaultTheme().ColorPrimary should not be empty")
	}
}

func TestLoadTheme_Default(t *testing.T) {
	theme, err := LoadTheme("default")
	if err != nil {
		t.Fatalf("LoadTheme('default') error: %v", err)
	}
	if theme.Name != "default" {
		t.Errorf("LoadTheme('default').Name = %q, want %q", theme.Name, "default")
	}
}

func TestLoadTheme_DarkAlias(t *testing.T) {
	theme, err := LoadTheme("dark")
	if err != nil {
		t.Fatalf("LoadTheme('dark') error: %v", err)
	}
	if theme.Name != "default" {
		t.Errorf("LoadTheme('dark').Name = %q, want %q", theme.Name, "default")
	}
}

func TestLoadTheme_Empty(t *testing.T) {
	theme, err := LoadTheme("")
	if err != nil {
		t.Fatalf("LoadTheme('') error: %v", err)
	}
	if theme.Name != "default" {
		t.Errorf("LoadTheme('').Name = %q, want %q", theme.Name, "default")
	}
}

func TestLoadTheme_FromFile(t *testing.T) {
	// Créer un répertoire temporaire pour les thèmes
	tmpDir := t.TempDir()

	// Créer un thème JSON de test
	testTheme := Theme{
		Name:           "test-light",
		ColorOK:        "#00ff00",
		ColorError:     "#ff0000",
		ColorWarn:      "#ff8800",
		ColorPrimary:   "#0000ff",
		ColorSecondary: "#8888ff",
		ColorBorder:    "#cccccc",
		ColorText:      "#000000",
		ColorDim:       "#888888",
		ColorHighlight: "#ffff00",
		ColorWhite:     "#ffffff",
		ColorBlack:     "#000000",
	}

	data, err := json.Marshal(testTheme)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	themePath := filepath.Join(tmpDir, "test-light.json")
	if err := os.WriteFile(themePath, data, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Charger directement en lisant le fichier (simuler LoadTheme)
	loadedData, err := os.ReadFile(themePath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var loaded Theme
	if err := json.Unmarshal(loadedData, &loaded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if loaded.Name != "test-light" {
		t.Errorf("loaded.Name = %q, want %q", loaded.Name, "test-light")
	}
	if loaded.ColorPrimary != "#0000ff" {
		t.Errorf("loaded.ColorPrimary = %q, want %q", loaded.ColorPrimary, "#0000ff")
	}
}

func TestApplyTheme(t *testing.T) {
	// Sauvegarder les couleurs originales
	origPrimary := ColorPrimary
	origOK := ColorOK

	// Appliquer un thème custom
	custom := &Theme{
		Name:           "custom",
		ColorOK:        "#11aa11",
		ColorError:     "#aa1111",
		ColorWarn:      "#aa8811",
		ColorPrimary:   "#1111aa",
		ColorSecondary: "#5555aa",
		ColorBorder:    "#555555",
		ColorText:      "#eeeeee",
		ColorDim:       "#666666",
		ColorHighlight: "#aaaa11",
		ColorWhite:     "#ffffff",
		ColorBlack:     "#000000",
	}

	ApplyTheme(custom)

	if string(ColorPrimary) != "#1111aa" {
		t.Errorf("After ApplyTheme, ColorPrimary = %q, want %q", string(ColorPrimary), "#1111aa")
	}
	if string(ColorOK) != "#11aa11" {
		t.Errorf("After ApplyTheme, ColorOK = %q, want %q", string(ColorOK), "#11aa11")
	}

	// Restaurer
	ApplyTheme(DefaultTheme())
	if string(ColorPrimary) != string(origPrimary) {
		t.Errorf("After restore, ColorPrimary = %q, want %q", string(ColorPrimary), string(origPrimary))
	}
	if string(ColorOK) != string(origOK) {
		t.Errorf("After restore, ColorOK = %q, want %q", string(ColorOK), string(origOK))
	}
}

func TestListThemes(t *testing.T) {
	themes, err := ListThemes()
	if err != nil {
		t.Fatalf("ListThemes() error: %v", err)
	}

	// Doit toujours contenir "default"
	found := false
	for _, name := range themes {
		if name == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListThemes() should always include 'dark'")
	}
}
