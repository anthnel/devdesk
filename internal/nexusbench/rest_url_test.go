package nexusbench

import "testing"

func TestRESTBaseURL(t *testing.T) {
	custom := Defaults()
	custom.HTTPNodePort = 30490
	tests := []struct {
		name string
		host string
		p    Params
		want string
	}{
		{"IPv4", "127.0.0.1", Defaults(), "http://127.0.0.1:30481"},
		{"host name", "datomir.home.arpa", Defaults(), "http://datomir.home.arpa:30481"},
		{"IPv6 gets its brackets", "::1", Defaults(), "http://[::1]:30481"},
		{"IPv6 already in brackets", "[::1]", Defaults(), "http://[::1]:30481"},
		{"IPv6 zone is escaped", "fe80::1%eth0", Defaults(), "http://[fe80::1%25eth0]:30481"},
		{"another port", "10.0.0.5", custom, "http://10.0.0.5:30490"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RESTBaseURL(tt.host, tt.p); got != tt.want {
				t.Errorf("RESTBaseURL(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

// The descriptor and the deployment build their URLs with the same function.
func TestRESTBaseURLMatchesTheDescriptor(t *testing.T) {
	p := Defaults()
	p.Hosts = []string{"127.0.0.1", "::1", "10.0.0.5"}
	d := BuildDescriptor(p, DeployResult{}, ProvisionResult{}, SeedResult{})
	for i, h := range p.Hosts {
		if want := RESTBaseURL(h, p); d.REST.URLs[i] != want {
			t.Errorf("descriptor URL %q, RESTBaseURL says %q", d.REST.URLs[i], want)
		}
	}
}
