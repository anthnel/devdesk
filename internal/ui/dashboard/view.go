package dashboard

import (
	"strings"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the dashboard as a grid of titled boxes. Il n'y a pas de cadre
// extérieur : toutes les autres vues encadrent un objet (une table, un
// formulaire), le dashboard est fait de groupes hétérogènes qu'un cadre unique
// n'aiderait pas à lire.
func (m Model) View() string {
	t := layoutTier(m.width, m.height)
	colWidth := t.columnWidth(m.width)
	columns := m.columnsFor(t)

	m.chartLines = m.fitCharts(columns, colWidth, t)
	inner := m.innerHeights(columns, colWidth, t)

	rendered := make([][]string, len(columns))
	height := 0
	for i, col := range columns {
		rendered[i] = m.renderColumn(col, colWidth, inner, t)
		if len(rendered[i]) > height {
			height = len(rendered[i])
		}
	}

	pad := theme.EmptyLineBg(sidePadding)
	gap := theme.EmptyLineBg(columnGap)
	blank := theme.EmptyLineBg(colWidth)

	// Une ligne vide sépare la règle de titre de la première rangée : sans
	// elle, la bordure haute des boîtes touche celle du titre et les deux se
	// lisent comme un cadre unique.
	lines := make([]string, 0, height+1)
	lines = append(lines, theme.EmptyLineBg(m.width))
	for i := range height {
		var line strings.Builder
		line.WriteString(pad)
		for c, col := range rendered {
			if c > 0 {
				line.WriteString(gap)
			}
			if i < len(col) {
				line.WriteString(col[i])
			} else {
				line.WriteString(blank)
			}
		}
		lines = append(lines, theme.PadWithBg(line.String(), m.width))
	}
	return strings.Join(lines, "\n")
}

// columnsFor distributes the active tab's sections over the palier's columns.
// À `wide`, la troisième colonne porte le contenu de l'onglet Resources : le
// palier décide où se trouve un fait, jamais s'il existe.
func (m Model) columnsFor(t tier) [][]section {
	overview, resources := overviewSections(), resourceSections()

	if m.activeTab == tabResources {
		return distribute(resources, t.columns())
	}
	if t.columns() == 3 {
		// À trois colonnes, la disposition est explicite plutôt que découpée en
		// parts égales : **les trois boîtes à graphes tiennent la première
		// rangée**. Elles ont la même hauteur — deux courbes chacune — et une
		// rangée est cadrée sur sa boîte la plus haute, donc les mélanger avec
		// les boîtes de texte laisserait du vide dans les deux rangées.
		code, health, host, dockerBox := overview[0], overview[1], overview[2], overview[3]
		network, storage := resources[0], resources[1]
		return [][]section{
			{host, code},
			{network, health},
			{dockerBox, storage},
		}
	}
	return distribute(overview, t.columns())
}

// distribute lays sections into `cols` columns, filling each column top to
// bottom — reading order down a column, which is how a stack of boxes is read.
func distribute(sections []section, cols int) [][]section {
	if cols < 1 {
		cols = 1
	}
	perColumn := (len(sections) + cols - 1) / cols

	out := make([][]section, 0, cols)
	for start := 0; start < len(sections); start += perColumn {
		out = append(out, sections[start:min(start+perColumn, len(sections))])
	}
	return out
}

// fitCharts measures how many lines a chart can take.
//
// La grille est rendue une première fois **sans aucune courbe** : ce qui reste
// entre ce squelette et la hauteur disponible est exactement ce que les courbes
// peuvent occuper. Elles vivent toutes dans la même rangée et il y en a deux par
// boîte, d'où le partage en deux.
//
// Mesurer plutôt que déduire est ce qui rend le calcul insensible aux boîtes :
// Health a gagné huit lignes en devenant un arbre, et aucune constante n'a eu
// à être mise à jour.
func (m Model) fitCharts(columns [][]section, width int, t tier) int {
	if t != tierWide {
		return 1
	}

	bare := m
	bare.chartLines = 0
	used := leadingBlank
	for _, height := range bare.innerHeights(columns, width, t) {
		used += height + theme.BoxChrome
	}

	// Le plancher est **une** ligne, pas trois : une courbe braille a besoin de
	// trois lignes pour valoir mieux qu'un sparkline, mais forcer trois lignes
	// quand il n'y en a que deux fait déborder la grille — et ce qui déborde
	// est perdu, pas repoussé. Une courbe basse vaut mieux qu'une ligne coupée.
	free := (m.height - used) / chartsPerBox
	return min(max(free, 1), maxChartHeight)
}

// chartHeightAt reports the chart height this model's size would produce, which
// is what the tests assert on: View() measures it on a copy, so it never
// survives on the model itself.
func (m Model) chartHeightAt(t tier) int {
	return m.fitCharts(m.columnsFor(t), t.columnWidth(m.width), t)
}

// innerHeight is the content height every box on screen fills, borders
// excluded: the tallest section of the frame.
//
// Elle est **dérivée des sections**, pas déclarée par le palier. Une constante
// pourrait tronquer une section qui grandit — un graphe ajouté en phase 3, une
// ligne ajoutée à Health en phase 4 — et la troncature ne se voit pas : la
// boîte reste bien formée, elle perd juste sa dernière ligne. Ce qui reste
// figé, et c'est le contrat de la phase 1, c'est qu'une section rende le même
// nombre de lignes quel que soit l'état de ses données.
// Elle est calculée **par rangée**, pas pour la grille entière : les boîtes de
// texte tiennent en six lignes et les boîtes à graphes en vingt, donc une
// hauteur unique laisserait treize lignes vides dans Health parce que Host, deux
// colonnes plus loin, porte deux courbes. Les rangées restent alignées entre
// elles, ce qu'une hauteur par boîte perdrait.
func (m Model) innerHeights(columns [][]section, width int, t tier) []int {
	rows := 0
	for _, col := range columns {
		rows = max(rows, len(col))
	}

	heights := make([]int, rows)
	for i := range heights {
		content := 0
		for _, col := range columns {
			if i >= len(col) {
				continue
			}
			content = max(content, len(col[i].render(m, width, t)))
		}
		heights[i] = max(nominalInnerHeight, content+trailingBlank)
	}
	return heights
}

// renderColumn stacks a column's boxes. Aucune ligne vide entre elles : leurs
// bordures séparent déjà, et à 30 lignes le budget vaut exactement deux boîtes.
func (m Model) renderColumn(sections []section, width int, inner []int, t tier) []string {
	var lines []string
	for i, s := range sections {
		height := nominalInnerHeight
		if i < len(inner) {
			height = inner[i]
		}
		content := padTo(s.render(m, width, t), height, theme.BoxContentWidth(width))
		lines = append(lines, theme.RenderTitledBox(s.title, content, width)...)
	}
	return lines
}

// padTo fills a section's content out to the frame's box height. Il ne tronque
// pas : innerHeight est calculée sur ces mêmes sections, donc une ligne perdue
// ici serait un bug de calcul, pas un débordement à absorber.
func padTo(content []string, height, innerWidth int) []string {
	out := make([]string, 0, height)
	out = append(out, content...)
	for len(out) < height {
		out = append(out, theme.EmptyLineBg(innerWidth))
	}
	return out
}

// filterComponents returns service components filtered by SSL type
func (m Model) filterComponents(sslOnly bool) []status.ComponentStatus {
	var result []status.ComponentStatus
	for _, c := range m.serviceComponents {
		isSSL := c.Type == status.TypeSSL
		if sslOnly == isSSL {
			result = append(result, c)
		}
	}
	return result
}

// countStatuses returns OK, Down, Error counts
func countStatuses(components []status.ComponentStatus) (ok, down, errCount int) {
	for _, c := range components {
		switch c.Status {
		case status.StatusOK:
			ok++
		case status.StatusDown:
			down++
		default:
			errCount++
		}
	}
	return
}

// HeaderView interface

// Frameless tells the router to draw no viewport border for this view. Le
// dashboard est la seule vue qui n'encadre pas un objet unique — voir View().
func (m Model) Frameless() bool {
	return true
}

// GetFooterHeight returns the footer height for this view (Rule 124): an empty
// line, an info line, and the tab bar when there is more than one tab.
func (m Model) GetFooterHeight() int {
	if m.showsTabBar() {
		return 3
	}
	return 2
}

// showsTabBar reports whether a tab bar is worth a line. Un seul onglet n'est
// pas un choix : la barre dirait « vous êtes ici », ce que l'écran dit déjà.
func (m Model) showsTabBar() bool {
	return tabCountFor(layoutTier(m.width, m.height)) > 1
}

// RenderFooter draws the tab bar (Rule 124).
//
// **Il n'y a pas de ligne « Updated »**, et son absence est un choix. Elle
// existait pour dater des valeurs figées à `-`, mais les trois horloges du
// dashboard tournent à la seconde, aux cinq secondes et à la trentaine : le
// plus vieux fait à l'écran n'a jamais une minute, et TimeAgo répondait donc
// `now` en permanence. Une ligne dont la valeur ne change jamais n'informe de
// rien, et coûtait la seule ligne d'information de la vue.
//
// L'information reste rendue même vide : le routeur budgète sur
// GetFooterHeight (Rule 124).
func (m Model) RenderFooter(width int) string {
	info := theme.EmptyLineBg(width)

	if !m.showsTabBar() {
		return theme.EmptyLineBg(width) + "\n" + info
	}

	tabs := []theme.TabItem{{Label: "Overview"}, {Label: "Resources"}}
	tabBar := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, int(m.activeTab)), width)
	return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + info
}

// GetShortcuts returns the keyboard shortcuts for the header
func (m Model) GetShortcuts() shortcut.Shortcuts {
	var shortcuts []shortcut.Shortcut
	// Rule 130: at `wide` the Resources boxes are on screen and there is no
	// second tab, so the key is not advertised.
	if tabCountFor(layoutTier(m.width, m.height)) > 1 {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "tab", Description: "Switch tab"})
	}
	shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"})
	if m.shared.IsAuthenticated {
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "R", Description: "Open MRs"},
			shortcut.Shortcut{Key: "I", Description: "Open issues"},
		)
	}
	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: "ctrl+p", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
	return shortcuts
}

// GetTitle returns the view title
func (m Model) GetTitle() string {
	return theme.IconDashboard + " Dashboard"
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent returns help content for the dashboard (Rule 114)
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Dashboard",
		Description: "The dashboard groups everything it knows into four titled boxes: Code, Health, Host and Docker. The layout follows the terminal's size — one column when it is narrow, a grid when it is not, and a third column on a large terminal, where it shows the Resources tab inline. A value that has not been measured yet reads '-', a source that is absent reads 'n/a', and a measured zero reads '0'.",
		KeyBindings: []help.KeyBinding{
			{Key: "tab", Description: "Switch between the Overview and Resources tabs"},
			{Key: "ctrl+r", Description: "Refresh all dashboard data"},
			{Key: "R", Description: "Open assigned merge requests in browser (requires authentication)"},
			{Key: "I", Description: "Open assigned issues in browser (requires authentication)"},
			{Key: "ctrl+p", Description: "Open command mode to navigate to other views"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Code",
				Body:  "Your GitLab activity — assigned merge requests, MRs awaiting your review, assigned issues — and the local clones under the workspaces directory. The two belong together: the explorer creates what does not exist, workspaces reconciles what does. Requires authentication via :gitlab-auth.",
			},
			{
				Title: "Health",
				Body:  "Monitored services (HTTP, ICMP, DNS) and SSL certificates, each with its up and down counts, plus the security posture read from the scan caches. Monitors are configured in :status, and the findings themselves live in :security — this box only counts them.",
			},
			{
				Title: "Host and Docker",
				Body:  "Two measurement points, and they are not on one axis: the Host box reads the machine dk runs on, while the Docker box reads inside the Docker Desktop VM, whose footprint is a subset of the host's. Both are true and they do not add up, which is why the box titles name where the number was measured.\n\nThe Host box ends on the tooling: one line when every tool is there, one node per missing tool otherwise — the name is what you need to install it. The Docker box counts what the daemon holds under a Resources root: images, volumes and networks. The sizes are not there, they are in Storage.",
			},
			{
				Title: "Resources",
				Body:  "The second tab carries the host and Docker series in detail — throughput, disk per volume, reclaimable space. It exists because nothing else shows them: there is no host view, and :containers shows one container rather than the machine. On a large terminal it is inline in a third column and the tab is a bigger version of it.",
			},
			{
				Title: "Navigation",
				Body:  "Press ctrl+p to open command mode, then type a view name: status for monitors, gitlab-auth for authentication, gitlab-explorer for browsing projects, workspaces for file management, containers for Docker management, oci-resources for OCI resource management, security for scanning.\n\nA bare : opens command mode too, but only when no text field has focus — inside one it types a colon, which values like https://trivy-server:4954 need. ctrl+p always works.",
			},
		},
	}
}
