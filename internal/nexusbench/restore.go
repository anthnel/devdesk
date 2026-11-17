package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// RestoreUpstream brings a cut upstream back (CA4): one ready replica, the proxy
// put back in service, the upstream images checked against the data set through a
// port-forward, and the cached images read once through docker-proxy, which
// refreshes the cache the unblock emptied (H-STALE). It never copies an image,
// never writes the cluster but the scale of the upstream, and never reads a
// non-PullThrough image through docker-proxy (F9). The stored credentials are
// read before anything is written. It runs no preflight (the caller runs
// OpRestore) and refuses a REST origin that is not the loopback. Every failure is
// a *BenchError, but a cancelled context, which is returned as is. A nil out
// discards the progress lines.
func RestoreUpstream(ctx context.Context, d SeedDeps, baseURL string, p Params, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.UpstreamIsBench() {
		return customUpstreamError(p)
	}
	if err := requireLoopbackBase(baseURL); err != nil {
		return err
	}
	_, pull, c, err := seedCredentials(ctx, d.Store, baseURL)
	if err != nil {
		return err
	}
	defer c.closeIdle()
	if err := EnsureUpstream(ctx, d.Runner, d.Clock, p, out); err != nil {
		return err
	}
	if err := putProxyBack(ctx, c, baseURL, p, out); err != nil {
		return err
	}
	images := Dataset()
	if err := verifyUpstreamImages(ctx, d, p, images, out); err != nil {
		return err
	}
	return verifyProxyImages(ctx, d, p, images, pull, out)
}

// verifyUpstreamImages reads each image of the upstream through a port-forward,
// with no credential, and checks its digest against the data set. The
// port-forward is stopped before it returns.
func verifyUpstreamImages(ctx context.Context, d SeedDeps, p Params, images []DatasetImage, out io.Writer) error {
	local, stop, err := d.Forwarder.Forward(ctx, p.Namespace, upstreamServiceName, upstreamServicePort)
	if err != nil {
		return stepFailure(ctx, err, "the port-forward to the upstream")
	}
	if stop != nil {
		defer func() { _ = stop() }()
	}
	if err := checkLoopbackForward(local); err != nil {
		return err
	}
	for _, img := range images {
		if img.Location != LocationUpstream {
			continue
		}
		name := img.Name + ":" + img.Tag
		have, err := d.Copier.ManifestDigest(ctx, upstreamImageRef(local, img), nil)
		if errors.Is(err, errImageAbsent) {
			return &BenchError{Kind: SeedFailed, Cause: "the upstream lost " + name, Check: resetCheck}
		}
		if err != nil {
			return stepFailure(ctx, err, "reading "+name+" in the upstream")
		}
		if have != img.Digest {
			return &BenchError{Kind: SeedFailed, Cause: "the upstream holds another content for " + name, Check: resetCheck}
		}
	}
	_, _ = fmt.Fprintln(out, "restore: the upstream holds the data set")
	return nil
}

// verifyProxyImages reads the PullThrough images through docker-proxy with the
// bench-pull account and checks their digests against the data set.
func verifyProxyImages(ctx context.Context, d SeedDeps, p Params, images []DatasetImage, pull Credentials, out io.Writer) error {
	for _, img := range images {
		if !img.PullThrough {
			continue
		}
		name := img.Name + ":" + img.Tag
		have, err := d.Copier.ManifestDigest(ctx, nexusImageRef(p, dockerProxyRepository, img), &pull)
		if err != nil {
			return stepFailure(ctx, err, "reading "+name+" through "+dockerProxyRepository)
		}
		if have != img.Digest {
			return &BenchError{Kind: SeedFailed, Cause: dockerProxyRepository + " serves another content for " + name, Check: resetCheck}
		}
	}
	_, _ = fmt.Fprintln(out, "restore: "+dockerProxyRepository+" serves the cached images")
	return nil
}
