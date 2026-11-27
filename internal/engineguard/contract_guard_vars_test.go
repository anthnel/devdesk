package engineguard_test

import (
	"fmt"
	"slices"
	"testing"
)

// Review of OC-09a1, B3: R2 also refuses a package-level var or const in a
// *_contract_test.go file. Its initializer runs when the test binary starts, before
// any requireEngine(t), in every run of the package, gate closed or not.
func TestG5RefusesAPackageLevelVarOrConstInAContractFile(t *testing.T) {
	const (
		contractFile = "internal/x/engine_contract_test.go"
		otherTest    = "internal/x/foo_test.go"
		support      = "internal/x/contract_support_test.go"
		head         = "package x\nimport \"testing\"\n"
		ok           = "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n}\n"
	)
	tests := []struct {
		name string
		file string
		src  string
		want []string
	}{
		{"a var whose value starts a command", contractFile,
			"package x\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\nvar _ = exec.Command(\"x\").Run()\n" + ok, []string{"6 R2"}},
		{"a var with a function call in a block", contractFile, head + "var (\n\tx = f()\n)\n" + ok, []string{"3 R2"}},
		{"a plain var", contractFile, head + "var x = 1\n" + ok, []string{"3 R2"}},
		{"a const", contractFile, head + "const c = 1\n" + ok, []string{"3 R2"}},
		{"a var and a const", contractFile, head + "var x = 1\nconst c = 2\n" + ok, []string{"3 R2", "4 R2"}},
		{"a type is allowed: it runs nothing", contractFile, head + "type row struct{ a int }\n" + ok, nil},
		{"imports only", contractFile, head + ok, nil},
		{"a var in another test file", otherTest, head + "var x = f()\n", nil},
		{"a var in the support file", support, head + "var x = f()\n", nil},
		{"a var inside a contract function is not package level", contractFile,
			head + "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n\tvar x = f()\n\t_ = x\n}\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := contractSites(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("contractSites: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%d %s", s.Line, s.Rule))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v\n%v", got, tt.want, sites)
			}
		})
	}
}
