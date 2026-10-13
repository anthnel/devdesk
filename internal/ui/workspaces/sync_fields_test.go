package workspaces

import (
	"reflect"
	"strings"
	"testing"
)

// Every field of Entry named Git* is git metadata that readGitStatus re-reads
// and a sync must put on the row. The test fills each one by reflection, so a
// field added to Entry without a line in copyGitFrom fails here instead of
// showing stale data until ctrl+r.
func TestCopyGitFromTakesEveryGitField(t *testing.T) {
	var from Entry
	v := reflect.ValueOf(&from).Elem()
	var names []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !strings.HasPrefix(f.Name, "Git") {
			continue
		}
		names = append(names, f.Name)
		switch v.Field(i).Kind() {
		case reflect.String:
			v.Field(i).SetString("x")
		case reflect.Int:
			v.Field(i).SetInt(7)
		case reflect.Bool:
			v.Field(i).SetBool(true)
		case reflect.Slice:
			v.Field(i).Set(reflect.ValueOf([]string{"2.4.2", "2.4"}))
		default:
			t.Fatalf("Entry.%s has kind %s: teach this test how to fill it", f.Name, v.Field(i).Kind())
		}
	}
	if len(names) == 0 {
		t.Fatal("no Git* field found on Entry")
	}

	var to Entry
	to.copyGitFrom(from)

	got := reflect.ValueOf(to)
	for _, name := range names {
		if !reflect.DeepEqual(got.FieldByName(name).Interface(), reflect.ValueOf(from).FieldByName(name).Interface()) {
			t.Errorf("copyGitFrom did not take Entry.%s: a sync would leave it stale until ctrl+r", name)
		}
	}
}
