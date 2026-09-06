// Package about is the `:about` screen — what this build of DevDesk is, and
// where it keeps its files.
//
// Ce que la vue montre est délibérément **le binaire, pas la machine**. Les
// versions de Trivy, de gitleaks ou de Docker sont de l'état de
// l'environnement : elles changent sans que DevDesk soit reconstruit, elles
// demandent d'aller les chercher, et le dashboard les affiche déjà. Ici rien
// n'est mesuré — tout est connu au démarrage — ce qui est aussi la raison pour
// laquelle l'écran n'offre pas de rafraîchissement : il n'y a rien à relire.
package about

import (
	"path/filepath"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

// SourceURL is where this project is developed.
const SourceURL = "https://github.com/anthnel/devdesk"

// field is one label/value line. An empty Value is rendered as "unknown"
// rather than as a blank, so a missing answer reads as a missing answer.
type field struct {
	Label string
	Value string
}

// section is a titled group of fields.
type section struct {
	Title  string
	Fields []field
}

// Model is the about screen. It holds no live state: the sections are computed
// once in New and never change, so Update only ever moves the scroll offset.
type Model struct {
	width, height int
	offset        int
	sections      []section
	footer        sharedcomponents.FooterMessage
}

// New builds the screen from the running build and the current configuration.
func New(cfg *config.Config) Model {
	return Model{sections: sections(version.Get(), cfg)}
}

// sections assembles what the screen shows, in reading order.
func sections(info version.Info, cfg *config.Config) []section {
	build := []field{
		{Label: "Version", Value: info.Version},
		{Label: "Commit", Value: commitField(info)},
		{Label: "Built", Value: buildDate(info.Date)},
		{Label: "Go", Value: info.Go},
		{Label: "Platform", Value: info.Platform},
	}

	return []section{
		{Title: "Build", Fields: build},
		{Title: "Paths", Fields: paths(cfg)},
		{Title: "Project", Fields: []field{{Label: "Source", Value: SourceURL}}},
	}
}

// commitField names the commit, and says when the tree it was built from had
// uncommitted changes — a dirty build is not reproducible from its SHA, which
// is exactly what someone reading this screen to report a bug needs to know.
func commitField(info version.Info) string {
	if info.Dirty && info.Commit != version.Unknown {
		return info.Commit + " (modified)"
	}
	return info.Commit
}

// buildDate renders an RFC 3339 stamp as a readable UTC minute.
//
// La chaîne brute est rendue telle quelle si elle ne parse pas : elle vient
// d'un `-ldflags` que rien ne valide, donc la refuser afficherait "unknown"
// pour une information qui est là.
func buildDate(raw string) string {
	if raw == "" || raw == version.Unknown {
		return version.Unknown
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format("2006-01-02 15:04 MST")
}

// paths lists the directories DevDesk reads and writes.
//
// Un chemin que l'application ne sait pas résoudre est rendu "unknown" plutôt
// qu'omis : la ligne absente laisserait croire que le fichier n'existe pas,
// alors que ce qui a échoué est la question du répertoire personnel.
func paths(cfg *config.Config) []field {
	dir, err := config.ConfigDir()
	if err != nil {
		return []field{{Label: "Config", Value: version.Unknown}}
	}

	log := version.Unknown
	if cfg != nil && cfg.App.LogFile != "" {
		log = cfg.App.LogFile
	}

	return []field{
		{Label: "Config", Value: dir},
		{Label: "Themes", Value: filepath.Join(dir, "themes")},
		{Label: "Cache", Value: filepath.Join(dir, "cache")},
		{Label: "Log", Value: log},
	}
}

// Init implements tea.Model. Nothing is fetched, so there is no command.
func (m Model) Init() tea.Cmd { return nil }

// Update handles resizing and scrolling, and nothing else.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.offset = m.clampOffset(m.offset)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	// Le composant consomme l'expiration qui lui est adressée (Rule 128). Cet
	// écran ne pose aucun message lui-même ; il en reçoit par la diffusion du
	// routeur (components.PostFooterMsg), qui est la seule voie qu'a le routeur
	// vers un footer.
	m.footer.Handle(msg)
	return m, nil
}

// handleKey moves the scroll offset. The viewport is driven by hand rather
// than by bubbles/viewport, whose default KeyMap carries the vim aliases this
// project removed (Rule 111).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		m.offset = m.clampOffset(m.offset - 1)
	case "down":
		m.offset = m.clampOffset(m.offset + 1)
	case "pgup":
		m.offset = m.clampOffset(m.offset - m.height)
	case "pgdown":
		m.offset = m.clampOffset(m.offset + m.height)
	case "home":
		m.offset = 0
	case "end":
		m.offset = m.clampOffset(len(m.lines()))
	}
	return m, nil
}

// clampOffset keeps the offset inside what there is to scroll. A body shorter
// than the viewport has nothing to scroll, and reports an offset of zero
// rather than a negative one.
func (m Model) clampOffset(offset int) int {
	maxOffset := len(m.lines()) - m.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// InEditMode implements the router's contract. Nothing on this screen takes
// text, so `:` and every other global key always reaches the router.
func (m Model) InEditMode() bool { return false }
