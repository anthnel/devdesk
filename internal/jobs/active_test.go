package jobs

import (
	"reflect"
	"testing"
)

// activeRun builds a run with one item per (target, state) pair.
func activeRun(id JobID, kind Kind, contextName string, items ...Item) Run {
	return Run{ID: id, Kind: kind, Context: contextName, Items: items}
}

func activeItem(target string, state ItemState) Item {
	return Item{Target: target, State: state}
}

func TestActiveOn(t *testing.T) {
	const ctx = "work"
	tests := []struct {
		name   string
		runs   []Run
		target string
		kinds  []Kind
		want   []Active
	}{
		{
			name:   "running item",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/api", ItemRunning))},
			target: "/w/api",
			want:   []Active{{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemRunning)}},
		},
		{
			name:   "queued item",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/api", ItemQueued))},
			target: "/w/api",
			want:   []Active{{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemQueued)}},
		},
		{
			name: "running first, then queued",
			runs: []Run{
				activeRun(1, KindScan, ctx, activeItem("/w/api", ItemQueued)),
				activeRun(2, KindSync, ctx, activeItem("/w/api", ItemRunning)),
			},
			target: "/w/api",
			want: []Active{
				{Kind: KindSync, RunID: 2, Item: activeItem("/w/api", ItemRunning)},
				{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemQueued)},
			},
		},
		{
			name: "same state keeps run order",
			runs: []Run{
				activeRun(1, KindScan, ctx, activeItem("/w/api", ItemRunning)),
				activeRun(2, KindSync, ctx, activeItem("/w/api", ItemRunning)),
				activeRun(3, KindDelete, ctx, activeItem("/w/api", ItemRunning)),
			},
			target: "/w/api",
			want: []Active{
				{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemRunning)},
				{Kind: KindSync, RunID: 2, Item: activeItem("/w/api", ItemRunning)},
				{Kind: KindDelete, RunID: 3, Item: activeItem("/w/api", ItemRunning)},
			},
		},
		{
			name: "settled items are excluded",
			runs: []Run{activeRun(1, KindScan, ctx,
				activeItem("/w/api", ItemDone), activeItem("/w/api2", ItemFailed), activeItem("/w/api3", ItemSkipped))},
			target: "/w/api",
		},
		{
			name: "settled states on the target itself",
			runs: []Run{
				activeRun(1, KindScan, ctx, activeItem("/w/api", ItemDone)),
				activeRun(2, KindSync, ctx, activeItem("/w/api", ItemFailed)),
				activeRun(3, KindPull, ctx, activeItem("/w/api", ItemSkipped)),
			},
			target: "/w/api",
		},
		{
			name:   "other context is excluded",
			runs:   []Run{activeRun(1, KindScan, "other", activeItem("/w/api", ItemRunning))},
			target: "/w/api",
		},
		{
			name:   "kind filter excludes other kinds",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/api", ItemRunning))},
			target: "/w/api",
			kinds:  []Kind{KindSync},
		},
		{
			name: "kind filter keeps the listed kinds",
			runs: []Run{
				activeRun(1, KindScan, ctx, activeItem("/w/api", ItemRunning)),
				activeRun(2, KindSync, ctx, activeItem("/w/api", ItemRunning)),
			},
			target: "/w/api",
			kinds:  []Kind{KindSync, KindDelete},
			want:   []Active{{Kind: KindSync, RunID: 2, Item: activeItem("/w/api", ItemRunning)}},
		},
		{
			name:   "trailing slash on the asked target",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/api", ItemRunning))},
			target: "/w/api/",
			want:   []Active{{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemRunning)}},
		},
		{
			name:   "trailing slash on the item target",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/api/", ItemRunning))},
			target: "/w/api",
			want:   []Active{{Kind: KindScan, RunID: 1, Item: activeItem("/w/api/", ItemRunning)}},
		},
		{
			name:   "image references are compared verbatim",
			runs:   []Run{activeRun(1, KindPull, ctx, activeItem("nginx:latest", ItemRunning))},
			target: "nginx",
		},
		{
			name:   "other target in the same run",
			runs:   []Run{activeRun(1, KindScan, ctx, activeItem("/w/web", ItemRunning), activeItem("/w/api", ItemQueued))},
			target: "/w/api",
			want:   []Active{{Kind: KindScan, RunID: 1, Item: activeItem("/w/api", ItemQueued)}},
		},
		{
			name: "open run is included",
			runs: []Run{{ID: 4, Kind: KindClone, Context: ctx, open: true,
				Items: []Item{activeItem("/w/api", ItemRunning)}}},
			target: "/w/api",
			want:   []Active{{Kind: KindClone, RunID: 4, Item: activeItem("/w/api", ItemRunning)}},
		},
		{name: "nil runs", runs: nil, target: "/w/api"},
		{name: "empty runs", runs: []Run{}, target: "/w/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActiveOn(tt.runs, ctx, tt.target, tt.kinds...)
			if len(got) == 0 && len(tt.want) == 0 {
				if got != nil {
					t.Errorf("ActiveOn = %#v, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ActiveOn = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestActiveOnReturnsACopy(t *testing.T) {
	in := activeItem("/w/api", ItemRunning)
	in.cancel = func() {}
	runs := []Run{activeRun(1, KindScan, "work", in)}
	got := ActiveOn(runs, "work", "/w/api")
	if len(got) != 1 {
		t.Fatalf("ActiveOn returned %d items, want 1", len(got))
	}
	if got[0].Item.cancel != nil {
		t.Error("the result carries an item's cancel function")
	}
	got[0].Item.State = ItemDone
	got[0].Item.Detail = "mutated"
	if runs[0].Items[0].State != ItemRunning || runs[0].Items[0].Detail != "" {
		t.Errorf("mutating the result changed the input run: %#v", runs[0].Items[0])
	}
}

func TestActiveOnIsStable(t *testing.T) {
	runs := []Run{
		activeRun(1, KindScan, "work", activeItem("/w/api", ItemQueued)),
		activeRun(2, KindSync, "work", activeItem("/w/api", ItemRunning)),
		activeRun(3, KindDelete, "work", activeItem("/w/api", ItemQueued)),
	}
	first := ActiveOn(runs, "work", "/w/api")
	for i := 0; i < 5; i++ {
		if again := ActiveOn(runs, "work", "/w/api"); !reflect.DeepEqual(first, again) {
			t.Fatalf("call %d differs: %#v vs %#v", i, again, first)
		}
	}
}
