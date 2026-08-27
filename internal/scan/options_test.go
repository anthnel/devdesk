package scan

import (
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// TestEveryConfiguredOptionReachesTheScanner is the test D26 is an instance of.
//
// ScanOptions used to be assembled by hand in three places — the security form,
// the workspaces list and the images list — and the three had drifted:
// IgnoreEOL was set in one of them, so ticking "ignore EOL" applied when
// scanning from the form and silently did not when scanning from either list.
//
// So this does not assert that IgnoreEOL is carried. It asserts that *every*
// field the two structs have in common is carried, by walking them: a tenth
// option added to ScanConfig and ScanOptions without plumbing it through fails
// here rather than shipping.
func TestEveryConfiguredOptionReachesTheScanner(t *testing.T) {
	shared := sharedFields()
	if len(shared) == 0 {
		t.Fatal("no field name is shared between ScanConfig and ScanOptions — the walk is broken, not the code")
	}

	cfg := config.ScanConfig{}
	cfgValue := reflect.ValueOf(&cfg).Elem()
	for _, name := range shared {
		cfgValue.FieldByName(name).Set(distinctValue(name, cfgValue.FieldByName(name).Type()))
	}

	got := reflect.ValueOf(OptionsFromConfig(&config.Config{Scan: cfg}))

	for _, name := range shared {
		want := distinctValue(name, cfgValue.FieldByName(name).Type()).Interface()
		if actual := got.FieldByName(name).Interface(); actual != want {
			t.Errorf("OptionsFromConfig did not carry %s: got %v, want %v", name, actual, want)
		}
	}
}

// TestOptionsFromConfigCarriesNothingItWasNotGiven pins the other direction: a
// zero config must produce zero options, so a default that only exists in the
// builder cannot hide a config field nobody set.
func TestOptionsFromConfigCarriesNothingItWasNotGiven(t *testing.T) {
	opts := OptionsFromConfig(&config.Config{})

	if opts.EnableVuln || opts.EnableSecret || opts.EnableMisconfig || opts.EnableLicense {
		t.Errorf("an empty config produced enabled scanners: %+v", opts)
	}
	if opts.TrivyImage != "" || opts.GitleaksImage != "" || opts.TrivyServer != "" {
		t.Errorf("an empty config produced tool settings: %+v", opts)
	}
	if opts.OnProgress != nil {
		t.Error("OptionsFromConfig set OnProgress; the caller owns it")
	}
}

// sharedFields lists the field names ScanConfig and ScanOptions have in common
// with the same type. Those are exactly the settings the builder must carry;
// anything else on either side is that struct's own business.
func sharedFields() []string {
	optionFields := make(map[string]reflect.Type)
	optionsType := reflect.TypeFor[ScanOptions]()
	for i := range optionsType.NumField() {
		f := optionsType.Field(i)
		optionFields[f.Name] = f.Type
	}

	var shared []string
	configType := reflect.TypeFor[config.ScanConfig]()
	for i := range configType.NumField() {
		f := configType.Field(i)
		if t, ok := optionFields[f.Name]; ok && t == f.Type {
			shared = append(shared, f.Name)
		}
	}
	return shared
}

// distinctValue builds a value that cannot be confused with a zero value or
// with another field's, so a builder that carries the wrong field is caught as
// surely as one that carries none.
func distinctValue(name string, t reflect.Type) reflect.Value {
	switch t.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(true)
	case reflect.String:
		return reflect.ValueOf("carried-" + name)
	default:
		panic("no distinct value for " + t.Kind().String() + " (" + name + ")")
	}
}
