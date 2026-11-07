package datatable

// ColumnShape is what a cross-view guard reads of a column without knowing the
// row type T: the declared nature of the column, not its cells.
type ColumnShape struct {
	Title    string
	Sizing   Sizing
	MinWidth int
	// Indicator, Less and Search report whether the column declares an activity
	// indicator, a comparator and a search text.
	Indicator, Less, Search bool
}

// Shapes returns the non-generic shape of every declared column, in order. A
// fresh slice is built on each call, so the caller cannot alter the table.
func (m *Model[T]) Shapes() []ColumnShape {
	shapes := make([]ColumnShape, len(m.cfg.Columns))
	for i, c := range m.cfg.Columns {
		shapes[i] = ColumnShape{
			Title:     c.Title,
			Sizing:    c.Sizing,
			MinWidth:  c.MinWidth,
			Indicator: c.Indicator != nil,
			Less:      c.Less != nil,
			Search:    c.Search != nil,
		}
	}
	return shapes
}

// ActivityTable is the face of a table a cross-view guard inspects. It is
// read-only: *Model[T] satisfies it for any T.
type ActivityTable interface {
	Shapes() []ColumnShape
	RenderedWidth() int
}
