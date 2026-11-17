package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
)

// benchRepositories are the only repositories reset may delete, in the order it
// deletes them: the group first, as it names the two others.
var benchRepositories = []string{dockerGroupRepository, dockerProxyRepository, dockerHostedRepository}

// repositoryPath is the path of one repository, whatever its kind.
func repositoryPath(name string) string { return restAPI + "/repositories/" + name }

// deleteRepository deletes one of benchRepositories. A repository that is not
// there is existed == false without an error. Any other name is refused before
// a request is sent: the bench never deletes what it did not create.
func (c *client) deleteRepository(ctx context.Context, name string) (existed bool, err error) {
	if !slices.Contains(benchRepositories, name) {
		return false, errors.New("refused to delete a repository that is not one of the bench")
	}
	op := "delete the repository " + name
	resp, err := c.do(ctx, request{op: op, method: http.MethodDelete, path: repositoryPath(name)})
	if err != nil {
		return false, err
	}
	switch {
	case resp.status == http.StatusNotFound:
		return false, nil
	case resp.status < 200 || resp.status > 299:
		return false, &APIError{Op: op, Status: resp.status}
	}
	return true, nil
}

// invalidatableRepositories are the only repositories invalidateCache may empty:
// the proxy and the group of the bench. A hosted repository has no cache.
var invalidatableRepositories = []string{dockerGroupRepository, dockerProxyRepository}

// invalidateCache empties the cache of the proxy or the group of the bench, the
// negative entries included. Any other name is refused before a request is sent,
// so that the name never decides the path the admin credentials are sent to.
func (c *client) invalidateCache(ctx context.Context, name string) error {
	if !slices.Contains(invalidatableRepositories, name) {
		return errors.New("refused to empty the cache of a repository that is not the proxy or the group of the bench")
	}
	_, err := c.expect(ctx, request{
		op: "invalidate the cache of " + name, method: http.MethodPost, path: repositoryPath(name) + "/invalidate-cache",
	})
	return err
}

// unblockProxy puts docker-proxy back in service after its upstream came back:
// it sends again the configuration Provision writes (never one read back from the
// instance), which lifts the automatic block, then invalidates the cache. A
// proxy that does not exist is an error: up creates it.
func (c *client) unblockProxy(ctx context.Context, p Params) error {
	return c.unblockProxyFrom(ctx, repositorySpecs(p))
}

// unblockProxyFrom is unblockProxy with the specifications it sends. Without
// one for docker-proxy it fails before any request: skipping the write and
// invalidating the cache would report a success that changed nothing.
func (c *client) unblockProxyFrom(ctx context.Context, specs []repositorySpec) error {
	i := slices.IndexFunc(specs, func(spec repositorySpec) bool { return spec.name == dockerProxyRepository })
	if i < 0 {
		return &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the bench has no specification for the proxy repository " + dockerProxyRepository,
			Check: "this is a defect of the bench: report it, with the output of `mise run nexus-bench:render`",
		}
	}
	path := dockerReposPath + "/proxy/" + dockerProxyRepository
	found, err := c.get(ctx, "read the proxy repository "+dockerProxyRepository, path, nil, nil)
	if err != nil {
		return err
	}
	if !found {
		return &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the proxy repository " + dockerProxyRepository + " does not exist",
			Check: upCheck,
		}
	}
	if err := c.write(ctx, "update the proxy repository "+dockerProxyRepository, http.MethodPut, path, specs[i].config); err != nil {
		return err
	}
	return c.invalidateCache(ctx, dockerProxyRepository)
}

// requireAdmin makes one read as admin, so that a stored password the instance
// refuses stops the caller before it changes anything.
func (c *client) requireAdmin(ctx context.Context) error {
	op := "check the admin account"
	_, err := c.get(ctx, op, usersPath, url.Values{"userId": {adminUserID}}, nil)
	var api *APIError
	if errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden) {
		return &BenchError{
			Kind:  ProvisionFailed,
			Cause: "Nexus refused the stored admin password, so nothing was changed",
			Check: "the password in the secrets file may not match the instance; run `mise run nexus-bench:down` then `mise run nexus-bench:up`",
			Err:   err,
		}
	}
	return err
}

// UnblockProxy puts docker-proxy back in service after its upstream came back:
// the configuration Provision writes, sent again, then its cache invalidated.
// It uses the stored admin password and nothing else, and refuses an upstream
// the bench did not deploy before any request. Every failure is a *BenchError.
// A nil out discards the progress lines.
func UnblockProxy(ctx context.Context, store SecretStore, baseURL string, p Params, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if !p.UpstreamIsBench() {
		return customUpstreamError(p)
	}
	if err := requireLoopbackBase(baseURL); err != nil {
		return err
	}
	c, _, err := adminFromStore(ctx, baseURL, store)
	if err != nil {
		return asBenchError(err, p, baseURL)
	}
	defer c.closeIdle()
	return putProxyBack(ctx, c, baseURL, p, out)
}

// putProxyBack is the request side of UnblockProxy, on a client the caller owns,
// with the progress lines.
func putProxyBack(ctx context.Context, c *client, baseURL string, p Params, out io.Writer) error {
	_, _ = fmt.Fprintln(out, "restore: putting "+dockerProxyRepository+" back in service")
	if err := c.unblockProxy(ctx, p); err != nil {
		return asBenchError(err, p, baseURL)
	}
	_, _ = fmt.Fprintln(out, "restore: "+dockerProxyRepository+" is back in service, its cache invalidated")
	return nil
}

// IsLoopbackRESTHost tells whether host is one of the two spellings of the
// loopback a restore or a reset accepts: 127.0.0.1 and ::1, exactly (brackets of
// the IPv6 literal aside). Any other loopback address (127.0.0.2, ::ffff:127.0.0.1,
// 0:0:0:0:0:0:0:1) is refused, and so is a name. The command checks its
// --rest-host with this predicate, so that it and the library hold one rule.
func IsLoopbackRESTHost(host string) bool {
	if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	return host == "127.0.0.1" || host == "::1"
}

// requireLoopbackBase refuses a REST origin whose host is not 127.0.0.1 or ::1,
// before a request carries the admin credentials to it: Nexus answers in plain
// HTTP, and what a restore or a reset does is local to the machine that runs the
// port-forwards. "localhost" is refused, as loopbackRegistry refuses it: a name
// resolves through the configuration of the host. The host is parsed, so that
// "127.0.0.1@evil.example" and "127.0.0.1.evil.example" are not the loopback, and
// an origin with user information is refused: the origin is to carry no credential
// of its own next to the admin one.
func requireLoopbackBase(baseURL string) error {
	if u, err := url.Parse(baseURL); err == nil && u.User == nil && IsLoopbackRESTHost(u.Hostname()) {
		return nil
	}
	return &BenchError{
		Kind:  Unclassified,
		Cause: "the REST origin is not on the loopback, and the admin credentials are sent in clear text, so this command does not send them there",
		Check: "run this command on the machine that runs the bench, or leave the REST host at its default (127.0.0.1)",
	}
}
