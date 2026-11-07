package datatable

import (
	"reflect"
	"testing"
)

// *Model[T] is the face a cross-view guard inspects.
var _ ActivityTable = (*Model[row])(nil)

func shapeConfig() Config[row] {
	return Config[row]{
		Key:        func(r row) string { return r.Name },
		SortColumn: 1,
		Columns: []Column[row]{
			IndicatorColumn(stateOf, nil, nil),
			{
				Title: "Name", Sizing: SizingContent, MinWidth: 12,
				Cell:   func(r row) string { return r.Name },
				Less:   func(a, b row) bool { return a.Name < b.Name },
				Search: func(r row) string { return r.Name },
			},
			{
				Title: "Size", Sizing: SizingFixed, MinWidth: 6,
				Cell: func(r row) string { return "" },
				Less: func(a, b row) bool { return a.Size < b.Size },
			},
			{
				Title: "Note", Sizing: SizingContent, MinWidth: 8,
				Cell:   func(r row) string { return "" },
				Search: func(r row) string { return "" },
			},
			{
				Title: "Plain", Sizing: SizingFixed, MinWidth: 4,
				Cell: func(r row) string { return "" },
			},
		},
	}
}

func wantShapes() []ColumnShape {
	return []ColumnShape{
		{Title: "", Sizing: SizingFixed, MinWidth: IconColumnWidth, Indicator: true},
		{Title: "Name", Sizing: SizingContent, MinWidth: 12, Less: true, Search: true},
		{Title: "Size", Sizing: SizingFixed, MinWidth: 6, Less: true},
		{Title: "Note", Sizing: SizingContent, MinWidth: 8, Search: true},
		{Title: "Plain", Sizing: SizingFixed, MinWidth: 4},
	}
}

func TestShapesReflectEveryColumnInOrder(t *testing.T) {
	m := New(shapeConfig())
	m.SetItems([]row{{Name: "a", Size: 1}, {Name: "b", Size: 2}})
	got := m.Shapes()
	if want := wantShapes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Shapes() = %+v\nwant %+v", got, want)
	}
}

func TestShapesOfAnEmptyTableAreStillReported(t *testing.T) {
	m := New(shapeConfig())
	if n := len(m.Items()); n != 0 {
		t.Fatalf("precondition: %d items, want 0", n)
	}
	if got, want := m.Shapes(), wantShapes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Shapes() on an empty table = %+v\nwant %+v", got, want)
	}
}

func TestShapesIndicatorColumnIsUntitledFixedIconWide(t *testing.T) {
	m := New(shapeConfig())
	s := m.Shapes()
	if len(s) == 0 {
		t.Fatal("Shapes() is empty")
	}
	c := s[0]
	if !c.Indicator || c.Title != "" || c.Sizing != SizingFixed || c.MinWidth != IconColumnWidth {
		t.Fatalf("column 0 shape = %+v, want untitled fixed IconColumnWidth indicator", c)
	}
	for i, o := range s[1:] {
		if o.Indicator {
			t.Errorf("column %d reports Indicator", i+1)
		}
	}
}

func TestShapesIsAReadOnlyCopy(t *testing.T) {
	m := New(shapeConfig())
	first := m.Shapes()
	if len(first) == 0 {
		t.Fatal("Shapes() is empty")
	}
	first[0].Title = "mutated"
	if got := m.Shapes()[0].Title; got != "" {
		t.Fatalf("mutating the result changed the table: title %q", got)
	}
}
