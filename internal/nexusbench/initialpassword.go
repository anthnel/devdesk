package nexusbench

import (
	"context"
	"fmt"
	"strings"
)

const (
	// nexusDeployment is the Deployment the manifests create.
	nexusDeployment = "deploy/nexus"
	// initialPasswordFile is where a fresh Nexus writes the password of admin.
	initialPasswordFile = "/nexus-data/admin.password"
)

// kubectlInitialPassword is the InitialPassword that reads the file in the pod
// with `kubectl exec`, through the Runner: this file starts no process.
type kubectlInitialPassword struct {
	runner    Runner
	namespace string
}

// NewKubectlInitialPassword returns the InitialPassword of the Nexus pod of
// namespace, read through r.
func NewKubectlInitialPassword(r Runner, namespace string) InitialPassword {
	return &kubectlInitialPassword{runner: r, namespace: namespace}
}

// Read implements InitialPassword. A missing file is (_, false, nil); any other
// failure is an error that carries neither the output of the command nor the
// password.
func (k *kubectlInitialPassword) Read(ctx context.Context) (string, bool, error) {
	stdout, stderr, err := k.runner.Run(ctx, nil, "-n", k.namespace, "exec", nexusDeployment, "--", "cat", initialPasswordFile)
	if err != nil {
		if strings.Contains(string(stderr), "No such file or directory") {
			return "", false, nil
		}
		kind := classifyKubectl(string(stderr))
		if kind == Unclassified {
			kind = ProvisionFailed
		}
		return "", false, &BenchError{
			Kind:  kind,
			Cause: "kubectl could not read " + initialPasswordFile + " in the Nexus pod",
			Check: fmt.Sprintf("check that the pod is Running: kubectl -n %s get pods -l app.kubernetes.io/name=nexus", k.namespace),
			Err:   err,
		}
	}
	password := strings.Trim(string(stdout), "\r\n")
	return password, password != "", nil
}
