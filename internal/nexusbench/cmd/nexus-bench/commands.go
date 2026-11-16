package main

import (
	"fmt"
	"reflect"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// render prints the manifests. It builds no dependency: it contacts nothing.
func (s *session) render() int {
	out, err := nexusbench.Render(s.p)
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.stdout.Write(out); err != nil {
		return s.fail(err)
	}
	return exitOK
}

// open resolves the secrets path and builds the runner and the store, in that
// order, before anything touches the cluster. Both are refused here when they
// cannot work: kubectl missing, a secrets path that git does not ignore.
func (s *session) open() (nexusbench.Runner, nexusbench.SecretStore, error) {
	if s.p.SecretsPath == "" {
		path, err := nexusbench.DefaultSecretsPath(s.p.Context, s.p.Namespace, s.getenv)
		if err != nil {
			return nil, nil, err
		}
		s.p.SecretsPath = path
	}
	runner, err := s.d.newRunner(s.p.Context)
	if err != nil {
		return nil, nil, err
	}
	store, err := s.d.newStore(s.p.SecretsPath)
	if err != nil {
		return nil, nil, err
	}
	return runner, store, nil
}

// up deploys the bench, provisions it, and only then writes the descriptor.
// Deploy runs the preflight of up itself.
func (s *session) up() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	s.warnIfClearText()
	deployed, err := s.d.deploy(s.ctx, runner, s.d.newProber(), s.d.clock, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	provisioned, err := s.d.provision(s.ctx, s.restURL(), s.d.newInitial(runner, s.p.Namespace), store, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	return s.writeDescriptor(deployed, provisioned)
}

// provision provisions a bench that is already running.
func (s *session) provision() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	s.warnIfClearText()
	pre, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpProvision)
	if err != nil {
		return s.fail(err)
	}
	provisioned, err := s.d.provision(s.ctx, s.restURL(), s.d.newInitial(runner, s.p.Namespace), store, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	return s.writeDescriptor(nexusbench.DeployResult{NodeIP: pre.NodeIP}, provisioned)
}

// down deletes the bench namespace. The descriptor is left alone: it is the
// versioned description of the bench, not part of one instance.
func (s *session) down() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	if err := s.d.destroy(s.ctx, runner, s.d.clock, store, s.p, s.stdout); err != nil {
		return s.fail(err)
	}
	return exitOK
}

// descriptorPath is where the descriptor is read and written.
func (s *session) descriptorPath() string {
	if s.p.DescriptorPath != "" {
		return s.p.DescriptorPath
	}
	return nexusbench.DescriptorPath()
}

// writeDescriptor writes the descriptor of a bench that was provisioned. It is
// called after a successful Provision only: a partial result is never described.
func (s *session) writeDescriptor(deployed nexusbench.DeployResult, provisioned nexusbench.ProvisionResult) int {
	path := s.descriptorPath()
	if err := nexusbench.WriteDescriptor(path, nexusbench.BuildDescriptor(s.p, deployed, provisioned)); err != nil {
		return s.fail(err)
	}
	_, _ = fmt.Fprintf(s.stdout, "nexus-bench: descriptor written to %s\n", path)
	if !describesDefaultBench(s.p) {
		s.warn("descriptor.json now describes a non-default bench; do not commit it")
	}
	return exitOK
}

// describesDefaultBench tells whether p is the bench the versioned descriptor
// describes. The timeouts, the paths and the EULA consent do not change what
// the bench is, so they are left out.
func describesDefaultBench(p nexusbench.Params) bool {
	normalize := func(p nexusbench.Params) nexusbench.Params {
		p.Timeout, p.DeleteTimeout = 0, 0
		p.SecretsPath, p.DescriptorPath = "", ""
		p.AcceptEULA = false
		if len(p.Hosts) == 0 {
			p.Hosts = nil
		}
		return p
	}
	return reflect.DeepEqual(normalize(p), normalize(nexusbench.Defaults()))
}
