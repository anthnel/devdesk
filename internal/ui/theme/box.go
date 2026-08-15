package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderTitledBox draws a box of exactly `width` cells per line, carrying its
// title on the top border. It returns len(lines)+2 lines: the titled top
// border, one line per content line, and the bottom border.
//
// Le contenu est séparé des bordures latérales par une colonne d'espace de
// chaque côté (BoxPadding), donc la largeur utile est BoxContentWidth(width).
// Le padding est **horizontal seulement** : une ligne vide en haut et en bas
// coûterait quatre lignes par rangée de boîtes, et à 30 lignes de terminal le
// budget en vaut exactement deux.
//
// Chaque ligne porte un fond explicite (Rule 115) et fait exactement `width`
// cellules (Rule 116) : le contenu est paddé, et tronqué s'il déborde — un
// cadre dont une ligne dépasse casse toutes les colonnes à sa droite.
func RenderTitledBox(title string, lines []string, width int) []string {
	if width < minBoxWidth {
		width = minBoxWidth
	}
	content := BoxContentWidth(width)

	borderStyle := lipgloss.NewStyle().
		Foreground(ColorViewportBorder).
		Background(ColorBackground)
	border := lipgloss.NormalBorder()
	side := borderStyle.Render(border.Left)
	pad := EmptyLineBg(BoxPadding)

	out := make([]string, 0, len(lines)+2+BoxTopPadding)
	out = append(out, RenderBorderTitle(title, width))
	for range BoxTopPadding {
		out = append(out, side+EmptyLineBg(width-2)+side)
	}
	for _, line := range lines {
		out = append(out, side+pad+PadWithBg(Truncate(line, content), content)+pad+side)
	}
	out = append(out, borderStyle.Render(
		border.BottomLeft+strings.Repeat(border.Bottom, width-2)+border.BottomRight))
	return out
}

// BoxPadding is the space between a box's side borders and its content, per
// side. BoxTopPadding is the blank line between the titled border and the first
// content line — le titre est *sur* la bordure, donc sans elle la première
// ligne se lit comme une continuation du titre.
//
// Il n'y a pas de padding bas : le titre n'est qu'en haut, et une ligne de plus
// coûterait deux lignes par rangée de boîtes sur un budget qui en compte 17.
const (
	BoxPadding    = 1
	BoxTopPadding = 1
)

// BoxChrome is what a box costs in lines beyond its content: two borders and
// the top padding. Les vues qui budgètent une hauteur comptent avec.
const BoxChrome = 2 + BoxTopPadding

// BoxContentWidth returns the usable width inside a box of the given total
// width — borders and padding removed.
func BoxContentWidth(width int) int {
	return max(width-2-2*BoxPadding, 1)
}

// RenderTitledRule draws a titled horizontal rule with no corners, of exactly
// `width` cells. C'est le titre d'une vue sans cadre : GetTitle() garde un
// lecteur là où RenderBorderTitle dessinerait le haut d'une boîte inexistante.
func RenderTitledRule(title string, width int) string {
	borderStyle := lipgloss.NewStyle().
		Foreground(ColorViewportBorder).
		Background(ColorBackground)
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorTitleFg).
		Background(ColorBackground).
		Bold(true)

	rendered := titleStyle.Render(title)
	// " " + title + " " puis le remplissage jusqu'à width.
	used := 1 + lipgloss.Width(rendered) + 1
	fill := max(width-used, 0)
	return Bg(" ") + rendered + Bg(" ") +
		borderStyle.Render(strings.Repeat(lipgloss.NormalBorder().Top, fill))
}

// minBoxWidth is the narrowest box that can still show two borders and a
// character between them.
const minBoxWidth = 3

// Truncate cuts a line to at most `width` cells. lipgloss.MaxWidth coupe en
// tenant compte des séquences ANSI : couper à la main baverait à l'intérieur
// d'un échappement et sur toutes les lignes suivantes (Rule 122). Toute vue qui
// assemble des colonnes doit passer par elle plutôt que par un slice.
func Truncate(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
