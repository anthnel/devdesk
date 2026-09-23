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
// setting the two structs have in common is carried, by walking them down to
// each tool's own fields: an option added to ScanConfig and ScanOptions without
// plumbing it through fails here rather than shipping.
func TestEveryConfiguredOptionReachesTheScanner(t *testing.T) {
	shared := sharedFields()
	if len(shared) == 0 {
		t.Fatal("no field name is shared between ScanConfig and ScanOptions — the walk is broken, not the code")
	}

	cfg := config.ScanConfig{}
	cfgValue := reflect.ValueOf(&cfg).Elem()
	for _, name := range shared {
		fill(cfgValue.FieldByName(name), name)
	}

	got := reflect.ValueOf(OptionsFromConfig(&config.Config{Scan: cfg}))

	for _, name := range shared {
		want := cfgValue.FieldByName(name).Interface()
		if actual := got.FieldByName(name).Interface(); !reflect.DeepEqual(actual, want) {
			t.Errorf("OptionsFromConfig did not carry %s:\n got %+v\nwant %+v", name, actual, want)
		}
	}
}

// TestOptionsFromConfigCarriesNothingItWasNotGiven pins the other direction: a
// zero config must produce zero options, so a default that only exists in the
// builder cannot hide a config field nobody set.
func TestOptionsFromConfigCarriesNothingItWasNotGiven(t *testing.T) {
	opts := OptionsFromConfig(&config.Config{})

	if !reflect.ValueOf(opts.Categories).IsZero() {
		t.Errorf("an empty config produced categories: %+v", opts.Categories)
	}
	if !reflect.ValueOf(opts.Tools).IsZero() || opts.TrivyServer != "" {
		t.Errorf("an empty config produced tool settings: %+v", opts.Tools)
	}
	if opts.OnProgress != nil {
		t.Error("OptionsFromConfig set OnProgress; the caller owns it")
	}
}

// TestTrivyServerIsGatedByItsCheckbox pins the point of the server switch: an
// address left in the config from a previous session must not put a scan into
// client-server mode on its own — only the checkbox does.
func TestTrivyServerIsGatedByItsCheckbox(t *testing.T) {
	cfg := &config.Config{}
	cfg.Scan.Tools.Trivy.Server.URL = "https://trivy:4954"

	if got := OptionsFromConfig(cfg).TrivyServer; got != "" {
		t.Errorf("TrivyServer = %q with the checkbox off, want empty", got)
	}

	cfg.Scan.Tools.Trivy.Server.Enabled = true
	if got := OptionsFromConfig(cfg).TrivyServer; got != "https://trivy:4954" {
		t.Errorf("TrivyServer = %q with the checkbox on, want the configured address", got)
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

// fill sets every leaf of v to a value that cannot be confused with a zero
// value or with another leaf's, so a builder that carries the wrong setting is
// caught as surely as one that carries none. It descends into structs and
// lists, which is where every tool's settings now live.
func fill(v reflect.Value, path string) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("carried-" + path)
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0), path+"[0]")
		v.Set(s)
	default:
		panic("no distinct value for " + v.Kind().String() + " (" + path + ")")
	}
}
