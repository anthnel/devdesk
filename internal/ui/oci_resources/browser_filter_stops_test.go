package ociresources

import (
	"slices"
	"testing"
)

func TestNextFilter(t *testing.T) {
	g := resultFilter{groupSlug: "prod"}
	a := resultFilter{entryKey: "a"}
	c := resultFilter{entryKey: "c"}
	stops := []resultFilter{g, a, c}
	tests := []struct {
		name  string
		stops []resultFilter
		cur   resultFilter
		want  resultFilter
	}{
		{"no stops, off", nil, resultFilter{}, resultFilter{}},
		{"no stops, a stale filter goes off", nil, a, resultFilter{}},
		{"off goes to the first stop", stops, resultFilter{}, g},
		{"a stop goes to the next", stops, g, a},
		{"the middle stop goes to the next", stops, a, c},
		{"the last stop goes back to off", stops, c, resultFilter{}},
		{"an unknown filter goes off", stops, resultFilter{entryKey: "zzz"}, resultFilter{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextFilter(tt.stops, tt.cur); got != tt.want {
				t.Errorf("nextFilter = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFilterStopsForTwoGroups(t *testing.T) {
	b := &RegistryBrowser{entries: []browserRegistryEntry{
		{key: "a1", Slug: "ga", ParentSlug: "ga"},
		{key: "a2", Slug: "ga", ParentSlug: "ga"},
		{key: "b1", Slug: "gb", ParentSlug: "gb"},
		{key: "b2", Slug: "gb", ParentSlug: "gb"},
	}}
	e := func(k string) resultFilter { return resultFilter{entryKey: k} }
	want := []resultFilter{{groupSlug: "ga"}, {groupSlug: "gb"}, e("a1"), e("a2"), e("b1"), e("b2")}
	if got := b.filterStopsFor([]string{"a1", "a2", "b1", "b2"}); !slices.Equal(got, want) {
		t.Errorf("filterStopsFor = %+v, want %+v", got, want)
	}
}

func TestFilterStopsFor(t *testing.T) {
	b := &RegistryBrowser{entries: []browserRegistryEntry{
		{key: "hub", Slug: "hub"},
		{key: "m1", Slug: "prod", ParentSlug: "prod"},
		{key: "m2", Slug: "prod", ParentSlug: "prod"},
		{key: "m3", Slug: "prod", ParentSlug: "prod"},
		{key: "solo", Slug: "solo"},
	}}
	g := resultFilter{groupSlug: "prod"}
	e := func(k string) resultFilter { return resultFilter{entryKey: k} }
	tests := []struct {
		name string
		keys []string
		want []resultFilter
	}{
		{"no key", nil, nil},
		{"one key", []string{"hub"}, nil},
		{"one key repeated", []string{"hub", "hub"}, nil},
		{"two lone registries", []string{"hub", "solo"}, []resultFilter{e("hub"), e("solo")}},
		{"a group and a foreign registry", []string{"m1", "m2", "hub"}, []resultFilter{g, e("m1"), e("m2"), e("hub")}},
		{"partial members, no foreign key, keep the group", []string{"m1", "m2"}, []resultFilter{g, e("m1"), e("m2")}},
		{"a group covering every member is dropped", []string{"m1", "m2", "m3"}, []resultFilter{e("m1"), e("m2"), e("m3")}},
		{"the order follows the keys", []string{"solo", "hub"}, []resultFilter{e("solo"), e("hub")}},
		{"duplicates collapse", []string{"hub", "solo", "hub", "solo"}, []resultFilter{e("hub"), e("solo")}},
		{"an unknown key is a registry", []string{"hub", "ghost"}, []resultFilter{e("hub"), e("ghost")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.filterStopsFor(tt.keys); !slices.Equal(got, tt.want) {
				t.Errorf("filterStopsFor(%v) = %+v, want %+v", tt.keys, got, tt.want)
			}
		})
	}
}
