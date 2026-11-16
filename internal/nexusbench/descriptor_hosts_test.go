package nexusbench

import (
	"slices"
	"testing"
)

// The hosts of the descriptor (DEVDESK-504, step 5 after step 4): the ones the
// user named, else the ones the deployment reached, else the fallback.
func TestBuildDescriptorHosts(t *testing.T) {
	tests := []struct {
		name      string
		explicit  []string
		deploy    DeployResult
		wantHosts []string
		wantREST  []string // the REST URLs, one per host
	}{
		{
			name:      "the explicit hosts win over the reached ones",
			explicit:  []string{"10.0.0.5"},
			deploy:    DeployResult{ReachableHosts: []string{"127.0.0.1"}, NodeIP: sampleNodeIP},
			wantHosts: []string{"10.0.0.5"},
			wantREST:  []string{"http://10.0.0.5:30481"},
		},
		{
			name:      "the hosts that answered, not the ones that did not",
			deploy:    DeployResult{ReachableHosts: []string{"127.0.0.1"}, NodeIP: sampleNodeIP},
			wantHosts: []string{"127.0.0.1"},
			wantREST:  []string{"http://127.0.0.1:30481"},
		},
		{
			name:      "only the node address answered",
			deploy:    DeployResult{ReachableHosts: []string{sampleNodeIP}, NodeIP: sampleNodeIP},
			wantHosts: []string{sampleNodeIP},
			wantREST:  []string{"http://" + sampleNodeIP + ":30481"},
		},
		{
			name:      "both answered",
			deploy:    sampleDeploy(),
			wantHosts: []string{"127.0.0.1", sampleNodeIP},
			wantREST:  []string{"http://127.0.0.1:30481", "http://" + sampleNodeIP + ":30481"},
		},
		{
			name:      "none reached: the loopback and the node address",
			deploy:    DeployResult{NodeIP: sampleNodeIP},
			wantHosts: []string{"127.0.0.1", sampleNodeIP},
			wantREST:  []string{"http://127.0.0.1:30481", "http://" + sampleNodeIP + ":30481"},
		},
		{
			name:      "none reached and no node address: the loopback alone",
			deploy:    DeployResult{},
			wantHosts: []string{"127.0.0.1"},
			wantREST:  []string{"http://127.0.0.1:30481"},
		},
		{
			name:      "an empty list of reached hosts is the fallback too",
			deploy:    DeployResult{ReachableHosts: []string{}, NodeIP: sampleNodeIP},
			wantHosts: []string{"127.0.0.1", sampleNodeIP},
			wantREST:  []string{"http://127.0.0.1:30481", "http://" + sampleNodeIP + ":30481"},
		},
		{
			name:      "an IPv6 host that answered gets its brackets",
			deploy:    DeployResult{ReachableHosts: []string{"::1"}},
			wantHosts: []string{"::1"},
			wantREST:  []string{"http://[::1]:30481"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			p.Hosts = tt.explicit
			d := BuildDescriptor(p, tt.deploy, sampleProvision())

			if !slices.Equal(d.Hosts, tt.wantHosts) {
				t.Errorf("Hosts = %q, want %q", d.Hosts, tt.wantHosts)
			}
			if !slices.Equal(d.REST.URLs, tt.wantREST) {
				t.Errorf("REST.URLs = %q, want %q", d.REST.URLs, tt.wantREST)
			}
			if len(d.DockerConnectors) != 1 || len(d.DockerConnectors[0].URLs) != len(tt.wantHosts) {
				t.Errorf("DockerConnectors = %+v, want one connector with one URL per host", d.DockerConnectors)
			}
		})
	}
}

func TestBuildDescriptorDoesNotAliasReachedHosts(t *testing.T) {
	reached := []string{"127.0.0.1"}
	d := BuildDescriptor(Defaults(), DeployResult{ReachableHosts: reached}, sampleProvision())
	reached[0] = "changed"
	if d.Hosts[0] != "127.0.0.1" {
		t.Errorf("Hosts = %q follows the slice of the deploy result", d.Hosts)
	}
}
