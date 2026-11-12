package datatable

import (
	"fmt"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// Benchmarks for DEVDESK-280: the cost of fit and of one frame, to hold the
// grapheme-cluster measuring to a budget.
//
//	go test ./internal/ui/datatable -run '^$' -bench 'BenchmarkFit|BenchmarkView' -benchmem -count=10

// fitSink keeps the compiler from discarding the result of fit.
var fitSink string

// BenchmarkFit measures fit over a fixed corpus, at a width that fits the
// cell and at one that cuts it.
func BenchmarkFit(b *testing.B) {
	corpora := []struct {
		name  string
		cells []string
	}{
		{"ascii", []string{"running", "192.168.1.240", "api", "2024-05-01", "8080/tcp", "healthy", "web-1", "ok"}},
		{"ascii-cut", []string{
			"registry.example.com/team/app:1.2.3", "a-rather-long-service-name", "/var/lib/docker/volumes/data",
			"feature/devdesk-280-vs16-width", "2024-05-01T10:20:30Z", "0.0.0.0:8080->80/tcp", "unhealthy", "restarting",
		}},
		{"cjk", []string{"日本語テキスト", "日本", "中文名称", "한국어", "テスト", "日本語", "サーバー", "名前"}},
		{"vs16", []string{"a❤️x", "✔️ ok", "❤️❤️❤️❤️", "☀️ sun", "1️⃣2️⃣3️⃣", "x✔️", "ab✔️cd✔️ef", "✔️"}},
	}
	for _, c := range corpora {
		for _, width := range []int{8, 20} {
			for _, head := range []bool{false, true} {
				name := fmt.Sprintf("%s/w%d/head=%t", c.name, width, head)
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						for _, cell := range c.cells {
							fitSink = fit(cell, width, head)
						}
					}
				})
			}
		}
	}
}

// viewSink keeps the compiler from discarding the frame.
var viewSink string

// benchConfig is an activity column and five text columns, one of them
// truncated from the head, the shape of a real container or workspace table.
func benchConfig() Config[benchRow] {
	text := func(title string, min int, get func(benchRow) string) Column[benchRow] {
		return Column[benchRow]{
			Title: title, Sizing: SizingContent, MinWidth: min,
			Cell: get,
			Less: func(a, b benchRow) bool { return get(a) < get(b) },
		}
	}
	image := text("Image", 12, func(r benchRow) string { return r.Image })
	image.TruncateHead = true
	return Config[benchRow]{
		Key:        func(r benchRow) string { return r.Name },
		SortColumn: 1,
		Columns: []Column[benchRow]{
			IndicatorColumn(func(r benchRow) indicator.State { return stateOf(row{State: r.State}) }, nil, nil),
			text("Name", 12, func(r benchRow) string { return r.Name }),
			image,
			text("State", 8, func(r benchRow) string { return r.State }),
			text("Ports", 8, func(r benchRow) string { return r.Ports }),
			text("Created", 10, func(r benchRow) string { return r.Created }),
		},
	}
}

type benchRow struct{ Name, Image, State, Ports, Created string }

// BenchmarkView renders one frame of a 120-wide, 40-row, 6-column table of
// realistic ASCII cells with the cursor on row 3.
func BenchmarkView(b *testing.B) {
	states := []string{"running", "idle", "done", "error"}
	items := make([]benchRow, 40)
	for i := range items {
		items[i] = benchRow{
			Name:    fmt.Sprintf("service-%02d", i),
			Image:   fmt.Sprintf("registry.example.com/team/app-%02d:1.%d.%d", i, i%5, i%7),
			State:   states[i%len(states)],
			Ports:   fmt.Sprintf("0.0.0.0:%d->80/tcp", 8000+i),
			Created: fmt.Sprintf("2024-05-%02d", 1+i%28),
		}
	}
	m := New(benchConfig())
	m.Resize(120, 46)
	m.SetItems(items)
	m.SetCursor(3)

	b.ReportAllocs()
	for b.Loop() {
		viewSink = m.View()
	}
}
