package imagesearch

import (
	"context"
	"errors"
	"slices"
	"testing"
)

var _ Searcher = SearcherFunc(nil)

type ctxKey struct{}

func TestSearcherFuncDelegates(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		target  Target
		query   Query
		images  []Image
		partial bool
		err     error
	}{
		{"results", Target{EntryKey: "a", Username: "u"}, Query{Text: "nginx", Limit: 5}, []Image{{Name: "nginx", Official: true}}, false, nil},
		{"partial", Target{EntryKey: "b"}, Query{Text: "redis"}, []Image{{Name: "redis"}}, true, nil},
		{"error", Target{}, Query{}, nil, false, errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), ctxKey{}, "v")
			var s Searcher = SearcherFunc(func(c context.Context, tg Target, q Query) ([]Image, bool, error) {
				if c.Value(ctxKey{}) != "v" {
					t.Error("context not forwarded")
				}
				if tg != tt.target {
					t.Errorf("target = %+v, want %+v", tg, tt.target)
				}
				if q != tt.query {
					t.Errorf("query = %+v, want %+v", q, tt.query)
				}
				return tt.images, tt.partial, tt.err
			})
			images, partial, err := s.Search(ctx, tt.target, tt.query)
			if !slices.Equal(images, tt.images) || partial != tt.partial || !errors.Is(err, tt.err) {
				t.Errorf("got (%v, %v, %v), want (%v, %v, %v)", images, partial, err, tt.images, tt.partial, tt.err)
			}
		})
	}
}
