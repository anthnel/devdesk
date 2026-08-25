package netdiag

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/netiface"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ifaceDataMsg carries one read of this machine's interfaces.
type ifaceDataMsg struct {
	interfaces []netiface.Interface
	err        error
}

// ifaceFetchTimeout bounds one read. The interfaces are a system call and the
// counters are another; neither waits on the network, so this is a backstop
// against a wedged platform call rather than a budget anything is expected to
// approach.
const ifaceFetchTimeout = 5 * time.Second

func fetchInterfacesCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ifaceFetchTimeout)
		defer cancel()
		ifaces, err := netiface.List(ctx)
		return ifaceDataMsg{interfaces: ifaces, err: err}
	}
}

// InterfacesModel is the tab that lists this machine's network interfaces.
//
// It was the Topology tab, which showed four sections read from ip and
// iptables in containers started with --network host — that is, the Docker
// Desktop VM's network and not the machine's (D57). Three of the four sections
// were removed rather than translated, because nothing portable produces them
// (§3.44); the route question moved to the Diagnostics pipeline, where it is
// asked about a target rather than in the abstract. What is left is what reads
// natively everywhere, so the tab is named after it.
type InterfacesModel struct {
	width  int
	height int

	table   datatable.Model[netiface.Interface]
	spinner spinner.Model

	loading bool
	// lastOK dates the rows, and loadErr says the most recent read failed.
	//
	// A failed read keeps the rows on screen: they are not wrong, they are
	// dated, and dropping them would leave the tab empty over a transient
	// failure. What has to be said is their age — a footer *message* cannot
	// carry it, since it expires after three seconds (Rule 128).
	lastOK  time.Time
	loadErr bool

	// footer is this tab's own message line. Each tab keeps one: a shared
	// instance would let a message set here outlive the switch away from it.
	footer components.FooterMessage
}

func newInterfacesModel() *InterfacesModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	return &InterfacesModel{
		loading: true,
		spinner: sp,
		table: datatable.New(datatable.Config[netiface.Interface]{
			Columns:    interfaceColumns(),
			SortColumn: 0,
		}),
	}
}

// interfaceColumns describes the interfaces table.
//
// The MAC is here because net.Interfaces() hands it over for nothing and the
// old tab threw it away — it is what a DHCP lease, a switch port and an asset
// inventory are all keyed on, so it is the field someone comes to this screen
// to copy.
func interfaceColumns() []datatable.Column[netiface.Interface] {
	name := datatable.Column[netiface.Interface]{
		Title: "Interface", Sizing: datatable.SizingContent, MinWidth: 16,
		Cell:   func(i netiface.Interface) string { return i.Name },
		Search: func(i netiface.Interface) string { return i.Name },
		Less:   func(a, b netiface.Interface) bool { return a.Name < b.Name },
	}
	state := datatable.Column[netiface.Interface]{
		Title: "State", Sizing: datatable.SizingFixed, MinWidth: 7,
		Cell:  func(i netiface.Interface) string { return i.State },
		Style: interfaceStateStyle,
		Less:  func(a, b netiface.Interface) bool { return a.State < b.State },
	}
	mtu := datatable.Column[netiface.Interface]{
		Title: "MTU", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 6,
		Cell: func(i netiface.Interface) string {
			if !i.HasMTU() {
				return "-"
			}
			return strconv.Itoa(i.MTU)
		},
		Style: func(i netiface.Interface) lipgloss.Style {
			if !i.HasMTU() {
				return theme.DimStyle
			}
			return lipgloss.Style{}
		},
		Less: func(a, b netiface.Interface) bool { return a.MTU < b.MTU },
	}
	mac := datatable.Column[netiface.Interface]{
		Title: "MAC", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 17,
		Cell:   macOrDash,
		Search: func(i netiface.Interface) string { return i.MAC },
		Style: func(i netiface.Interface) lipgloss.Style {
			if i.MAC == "" {
				return theme.DimStyle
			}
			return lipgloss.Style{}
		},
	}
	rx := errorColumn("RX err", func(i netiface.Interface) *uint64 { return i.RxErrors })
	tx := errorColumn("TX err", func(i netiface.Interface) *uint64 { return i.TxErrors })
	// Both address columns follow their content and both carry a flex weight.
	// The flex is what levels them against each other when there is a shortfall
	// — a table reclaims from the widest content column first, so with the flex
	// on IPv6 alone it used to absorb the whole shortfall and render at zero
	// width from about 100 columns down, header included. IPv6 asks for more
	// and grows faster because its notation is longer.
	//
	// Squeezed to nothing is no longer one of the outcomes (§3.45): MinWidth is
	// a floor, and what gives way past it is a whole column, MTU / MAC / the
	// two counters first because they are the ones marked Optional here.
	v4 := addressColumn("IPv4", minIPv4Width, 1, netiface.Interface.IPv4List)
	v6 := addressColumn("IPv6", minIPv6Width, 2, netiface.Interface.IPv6List)

	return []datatable.Column[netiface.Interface]{name, state, mtu, mac, rx, tx, v4, v6}
}

// The address columns' minimum widths, from what the notation can hold:
// 255.255.255.255/32 is 18 characters, and a full IPv6 with its prefix length
// is 43. The IPv6 minimum is deliberately short of that — a column wide enough
// for the worst case would take half an 80-column terminal to serve the rare
// machine that has one, and the solver gives it the slack when there is any
// (Rule 116).
//
// The cliff §3.49 wrote down here is gone. It said that below about 88 columns
// the six exact-width columns took everything and both address columns were
// squeezed out — because MinWidth was an ask rather than a floor, and giving
// the solver one would have changed every table in the application. §3.45
// changed every table in the application. What gives way now is MTU, then MAC,
// then the two counters: they declare Optional, so they are removed whole and
// hand their width *and* their padding to the addresses.
const (
	minIPv4Width = 18
	minIPv6Width = 22
)

// addressColumn builds one family's column.
//
// An interface with no address of this family renders a dim dash rather than an
// empty cell: a machine with IPv4 only has no IPv6 address, which is a fact
// about it, and a blank cell reads as a reading that failed. It is the same
// distinction the MAC column already makes.
//
// Neither column sorts. A list of addresses has no order anyone means, and
// sorting on the joined string would rank 10.x above 9.x — a lexical answer to
// a question nobody asked in text.
func addressColumn(title string, minWidth, flex int, list func(netiface.Interface) string) datatable.Column[netiface.Interface] {
	return datatable.Column[netiface.Interface]{
		Title: title, Sizing: datatable.SizingContent, MinWidth: minWidth, Flex: flex,
		Cell: func(i netiface.Interface) string {
			if s := list(i); s != "" {
				return s
			}
			return "-"
		},
		Search: list,
		Style: func(i netiface.Interface) lipgloss.Style {
			if list(i) == "" {
				return theme.DimStyle
			}
			return lipgloss.Style{}
		},
	}
}

// macOrDash renders the hardware address, or says it has none.
//
// A loopback and a tunnel have no MAC, which is a fact about them rather than a
// missing reading — and an empty cell would look like the second.
func macOrDash(i netiface.Interface) string {
	if i.MAC == "" {
		return "-"
	}
	return i.MAC
}

// errorColumn builds one of the two counter columns.
//
// The dash is D58 in a cell: the counters come from a source that fails on its
// own, and a zero written because nobody looked reads as an interface with no
// errors. So nil prints "-" and never "0", and the dash is dim because an
// absent reading is not a value worth spotting — a non-zero count is.
func errorColumn(title string, get func(netiface.Interface) *uint64) datatable.Column[netiface.Interface] {
	return datatable.Column[netiface.Interface]{
		Title: title, Sizing: datatable.SizingFixed, Optional: true, MinWidth: 8,
		Cell: func(i netiface.Interface) string {
			n := get(i)
			if n == nil {
				return "-"
			}
			return strconv.FormatUint(*n, 10)
		},
		Style: func(i netiface.Interface) lipgloss.Style {
			n := get(i)
			if n == nil || *n == 0 {
				return theme.DimStyle
			}
			return theme.StatusWarningStyle
		},
		Less: func(a, b netiface.Interface) bool {
			return errorCount(get(a)) < errorCount(get(b))
		},
	}
}

// errorCount orders a counter for the sort. An unread counter sorts below zero
// so that descending — the direction anyone sorting this column wants — puts
// the interfaces that actually dropped frames on top, and never a row nobody
// managed to read.
func errorCount(n *uint64) int64 {
	if n == nil {
		return -1
	}
	return int64(*n)
}

// interfaceStateStyle colours the state, which is the column this table is
// scanned down. Down is the one worth spotting; a loopback is neither news nor
// a problem.
func interfaceStateStyle(i netiface.Interface) lipgloss.Style {
	switch i.State {
	case netiface.StateDown, netiface.StateLoop:
		return theme.DimStyle
	default:
		return theme.StatusOKStyle
	}
}

// initInterfaces returns the initial commands: one read, and the spinner.
func (im *InterfacesModel) initInterfaces() tea.Cmd {
	return tea.Batch(fetchInterfacesCmd(), im.spinner.Tick)
}

// InEditMode reports whether a text field has the keyboard.
func (im *InterfacesModel) InEditMode() bool {
	return im.table.FilterBar().InEditMode()
}

func (im *InterfacesModel) resize(width, height int) {
	im.width = width
	im.height = height
	// The full viewport width, borders included: Resize subtracts them itself, so
	// the `-2` this used to carry subtracted them twice and the selected row
	// stopped two cells short of the right border at every width. The
	// `max(..., 20)` went with it: a floor above what the terminal has makes the
	// table wider than the viewport, which is the overflow Rule 116 is about, and
	// a narrow terminal is now handled by dropping columns (§3.45).
	im.table.Resize(width, max(height, 3))
}

func (im *InterfacesModel) update(msg tea.Msg) (*InterfacesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ifaceDataMsg:
		return im.handleData(msg)
	case spinner.TickMsg:
		if !im.loading {
			return im, nil
		}
		var cmd tea.Cmd
		im.spinner, cmd = im.spinner.Update(msg)
		// The load is reported in the footer, so the frame has to reach it —
		// a spinner stuck on frame zero reads as a hang (Rule 139).
		im.footer.SetSpinnerFrame(im.spinner.View())
		return im, cmd
	case tea.KeyMsg:
		return im.handleKey(msg)
	}
	im.footer.Handle(msg)
	return im, nil
}

func (im *InterfacesModel) handleData(msg ifaceDataMsg) (*InterfacesModel, tea.Cmd) {
	im.loading = false

	if msg.err != nil {
		log.Printf("ERROR [netdiag/interfaces] list: %v", msg.err)
		im.loadErr = true
		return im, nil
	}

	im.loadErr = false
	im.lastOK = time.Now()
	im.table.SetItems(msg.interfaces)
	return im, nil
}

func (im *InterfacesModel) handleKey(msg tea.KeyMsg) (*InterfacesModel, tea.Cmd) {
	if im.table.FilterBar().InEditMode() {
		return im, im.table.Update(msg)
	}

	if msg.String() == "ctrl+r" {
		im.loading = true
		return im, tea.Batch(fetchInterfacesCmd(), im.spinner.Tick)
	}

	return im, im.table.Update(msg)
}

// statusLine is what this tab derives on every frame.
//
// Staleness outranks the load: a read that failed makes the rows on screen
// dated, and that has to keep being said while the next one runs.
func (im *InterfacesModel) statusLine() components.Status {
	if im.loadErr {
		return components.Status{Text: im.staleLabel(), Level: components.LevelError}
	}
	if im.loading {
		return components.Status{Text: "Reading network interfaces...", Spinner: true}
	}
	return components.Status{}
}

// staleLabel dates the rows on screen, or says there are none to date.
func (im *InterfacesModel) staleLabel() string {
	if im.lastOK.IsZero() {
		return "The network interfaces could not be read"
	}
	return "The network interfaces could not be read — interfaces as of " + theme.TimeAgo(im.lastOK)
}

// view renders the table. The body is never replaced by a spinner (Rule 139):
// the table keeps its header and its columns while it refreshes, and the load
// is a footer status.
func (im *InterfacesModel) view() string {
	if len(im.table.Visible()) == 0 && !im.loading && !im.table.FilterBar().IsVisible() {
		return theme.DimStyle.Render("No network interfaces found")
	}
	return im.table.View()
}

// summaryLine counts what is on screen, for the header.
func (im *InterfacesModel) summaryLine() string {
	up := 0
	for _, i := range im.table.Items() {
		if i.State == netiface.StateUp {
			up++
		}
	}
	return fmt.Sprintf("%d up / %d total", up, len(im.table.Items()))
}
