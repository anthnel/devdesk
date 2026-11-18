# Work behind a proxy

DevDesk follows the standard proxy variables of your shell. This guide says which
ones, what each part of DevDesk does with them, and what to do when a scanner
container cannot reach your proxy. For the design, see
[Network](../explanation/network.md#the-proxy-variables) and
[Security Scanning](../explanation/scanning.md#behind-a-proxy).

## Set the variables

Export them before you start DevDesk:

```bash
export HTTPS_PROXY=http://user:secret@proxy.corp.example:3128
export HTTP_PROXY=http://proxy.corp.example:3128
export NO_PROXY=localhost,.corp.example,10.0.0.0/8
dk
```

What DevDesk reads:

| Variable | Notes |
|---|---|
| `HTTPS_PROXY`, `https_proxy` | the proxy for `https://` targets |
| `HTTP_PROXY`, `http_proxy` | the proxy for `http://` targets |
| `NO_PROXY`, `no_proxy` | what goes direct |

- The upper case name wins over the lower case one. An empty value counts as not
  set.
- `ALL_PROXY` is not read.
- DevDesk reads the variables **once, when it starts**. A context switch does not
  read them again. After you change a variable, restart DevDesk.
- `NO_PROXY` takes a host name (it covers its subdomains), `.suffix` (subdomains
  only), an IP address, a CIDR, `host:port`, and `*`.
- `localhost` and loopback addresses always go direct. `*.localhost` names do not:
  they go through the proxy.

## Monitors

The HTTP and HTTPS monitors and the SSL monitor take the same path, so a host is
no longer green on HTTP and red on SSL behind a proxy. A `socks5` proxy works for
the HTTPS monitor, and the SSL check refuses it. When the proxy refuses the
`CONNECT`, select the DOWN row on the Certificates tab: the footer says which step
failed.

## Diagnostics

The Diagnostics measure the **direct** path from this machine, on purpose. They
never send a check to a proxy. The **Proxy** row tells you whether a proxy
variable applies to the target: configured (with its address, without any
credential), excluded by `NO_PROXY`, not used for a loopback target, invalid, or
none. When the direct connection fails and a proxy applies, the TCP row says so:
a closed direct path is expected behind a mandatory proxy.

## Scanner containers

When a scanner runs from an image (Docker or Podman), DevDesk passes `HTTP_PROXY`,
`HTTPS_PROXY` and `NO_PROXY` to the container, in both casings. The values travel
in the environment of the engine command. They never appear in the command line,
in `:jobs` or in a log, and DevDesk removes proxy credentials from the tool's error
output.

Two things to know:

- The proxy and its credentials go to every scanner image, including an image you
  set with `tool.Image`. Use only images you trust.
- DevDesk does not validate the values and does not rewrite them.

### The loopback warning

A container has its own loopback. If a variable names `127.0.0.1`, `localhost` or
`::1`, the container cannot reach it, and a scan that starts shows this warning in
the footer:

```text
HTTPS_PROXY is http://127.0.0.1:3128, on this machine's loopback: scanner containers cannot reach it
```

The scan still starts and will probably fail to connect. Choose one:

1. Make the proxy reachable from the container: listen on an address the
   container can route to (not only `127.0.0.1`), and set the variable to that
   address.
2. Run the tools as local binaries: set `scan.tools.<tool>.source: binary` (see
   [Locating the tools](../explanation/scanning.md#locating-the-tools)). A binary
   runs on this machine and reaches the local proxy.

The warning can be wrong. If your engine runs containers on the host network
(for example Podman with `netns = "host"` in `containers.conf`), the proxy is
reachable and you can ignore it. It can also name a container the run does not
use: a scan of an image runs only Trivy.

## Image pulls and the engine

The Docker or Podman daemon pulls images, with **its own** proxy configuration:
the service environment on Linux, the Proxies settings of Docker Desktop, the
configuration of the Podman machine. DevDesk does not set it. If a pull fails
behind a proxy, fix the engine's configuration. DevDesk's own registry calls
follow the variables above.

## git

Cloning and syncing run `git`, which reads the variables with libcurl's rules, not
DevDesk's:

- an upper case `HTTP_PROXY` is ignored for `http://` URLs (use `http_proxy`);
- `all_proxy` and `ALL_PROXY` are read;
- CIDR ranges in `NO_PROXY` depend on your curl version;
- `http.proxy` in your git configuration wins over the variables.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| A variable you changed has no effect | DevDesk reads them once | Restart DevDesk |
| The Proxy row says `Invalid HTTPS_PROXY value` | The value is not a usable proxy URL | Write it as `http://host:port` |
| Footer warning about a loopback proxy | The container cannot reach this machine's loopback | See [the loopback warning](#the-loopback-warning) |
| Hosts in `NO_PROXY` still go through the proxy | `*.localhost`, or a form the matcher does not know | Check the form against the list above |
