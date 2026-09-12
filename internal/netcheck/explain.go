package netcheck

import "fmt"

// Explanation is what a check means and what to do about it.
//
// It lives here rather than in the view for two reasons. The knowledge is about
// the domain, not the layout — "an incomplete chain breaks other clients" is
// true whether it is printed in a table or serialized to a tool call. And PR 4
// serializes checks together with their explanations: were this in the UI layer,
// whatever consumes those checks would have to import a Bubble Tea view.
//
// Nothing here reaches a model. §3.10 concedes the point about its own later
// stages — once the deterministic summariser exists it answers most of the
// question without one — and this is that summariser.
type Explanation struct {
	// Observed is what the check saw. It is the check's own summary: there is
	// one statement of what happened, and it is not restated in different words
	// somewhere else.
	Observed string
	// Means is why it matters. Always present.
	Means string
	// Do is the next action. Empty when there is nothing to do — a passing
	// check that invents an action teaches the user to ignore the field.
	Do string
}

// guidance is one row of the table: what a verdict means for a check, and what
// to do about it.
//
// It is keyed on reason as well as verdict because the remedies diverge where
// the verdict does not. A chain Fail is three different jobs — install a CA,
// fix the server's chain file, or find out what is intercepting the connection
// — and one paragraph covering all three would be useless for each.
type guidance struct {
	check   CheckID
	verdict Verdict
	reason  Reason // empty matches any reason, and is the fallback
	means   string
	do      string
}

// Explain returns what a check means and what to do about it.
func Explain(c Check) Explanation {
	e := Explanation{Observed: c.Summary}

	if c.Verdict == NotApplicable {
		e.Means, e.Do = explainNotApplicable(c)
		return e
	}
	if g, ok := guidanceFor(c.ID, c.Verdict, c.Reason); ok {
		e.Means, e.Do = g.means, g.do
		return e
	}

	// A check with no guidance is a gap in the table, not a state the user
	// should have to interpret. TestEveryCheckAScenarioProducesIsExplained is
	// what keeps this branch unreachable.
	e.Means = fmt.Sprintf("No guidance is written for %s at %s.", c.ID, c.Verdict)
	return e
}

// explainNotApplicable covers the two senses of NotApplicable in one place,
// because the difference is structural rather than per-check: Because is set
// when something upstream failed, and empty when the question has no meaning
// for this target.
func explainNotApplicable(c Check) (means, do string) {
	if c.Because == "" {
		if c.Reason == ReasonNotTLS {
			return "The port answered, but not with TLS. There is no certificate to inspect, " +
					"which is normal for a port that does not serve it.",
				"If you expected TLS here, check that you named the right port."
		}
		return "This question has no meaning for this target, so nothing was checked.", ""
	}

	blocker := checkTitles[c.Because]
	if blocker == "" {
		blocker = string(c.Because)
	}
	return fmt.Sprintf(
			"This could not be checked: %s did not succeed, and everything below it depends on that answer.",
			blocker),
		fmt.Sprintf("Fix %s first — this check will have an answer once it does.", blocker)
}

// guidanceFor looks up the most specific row: an exact reason match first, then
// the reason-less fallback for the same verdict.
func guidanceFor(id CheckID, v Verdict, r Reason) (guidance, bool) {
	var fallback guidance
	var haveFallback bool
	for _, g := range explanations {
		if g.check != id || g.verdict != v {
			continue
		}
		if g.reason == r {
			return g, true
		}
		if g.reason == "" {
			fallback, haveFallback = g, true
		}
	}
	return fallback, haveFallback
}

// explanations is the table. English US throughout (Rule 129).
//
// Two rules held while writing it, and both are checked:
//
//   - Means states a consequence, not a restatement of the summary. "The name
//     does not resolve" is the observation; "nothing else can be attempted, and
//     any client on this resolver will fail the same way" is what it means.
//   - Do is empty on a pass. An action invented for a green row is noise that
//     teaches the reader to skip the field on the rows that have one.
var explanations = []guidance{
	// --- resolution ---
	{check: CheckResolve, verdict: OK,
		means: "The name resolves, so the address the rest of these checks used is the one " +
			"your own traffic would use."},
	{check: CheckResolve, verdict: Fail,
		means: "Nothing below this can be attempted, and any client using this resolver fails " +
			"the same way. The name is wrong, the zone does not publish it, or this resolver " +
			"cannot see the zone at all.",
		do: "Check the spelling first, then try the same name against another resolver — if it " +
			"resolves there, the problem is which resolver you are pointed at, not the name."},
	{check: CheckResolve, verdict: Fail, reason: ReasonServerMisbehaving,
		means: "This resolver answered, but not with something usable — not a timeout, not a " +
			"missing name. It is commonly a router's built-in DNS proxy failing against the " +
			"queries this pipeline has to send to reach a resolver you name.",
		do: "Leave the resolver field empty to use the system resolver instead, or check this " +
			"one directly (`dig @<server> <host>`) to see what it actually sent back."},

	{check: CheckReverseDNS, verdict: OK,
		means: "The address publishes a name, which is what mail servers and some access " +
			"controls check."},
	{check: CheckReverseDNS, verdict: Warn,
		means: "No PTR record. Most services do not care, but mail delivery and some " +
			"logging and access-control systems reject or flag addresses without one.",
		do: "Nothing, unless this host sends mail or something in front of it requires a " +
			"reverse record."},

	// --- reachability ---
	{check: CheckICMP, verdict: OK,
		means: "The host answers echo requests, so it is up and the path to it carries ICMP."},
	{check: CheckICMP, verdict: Warn, reason: ReasonNoReply,
		means: "This does not mean the host is down. Firewalls and cloud providers drop echo " +
			"requests routinely, and the TCP connect below is the answer that counts.",
		do: "Read the TCP result rather than this one. Only worry about it if you rely on " +
			"ping for monitoring."},
	{check: CheckICMP, verdict: Warn, reason: ReasonPartialLoss,
		means: "Some probes came back and some did not. Genuine loss on the path degrades " +
			"everything running over it, but rate-limited ICMP looks identical from here.",
		do: "Run it again. Loss that repeats at the same rate is worth tracing; loss that " +
			"disappears was rate limiting."},
	{check: CheckICMP, verdict: Unknown,
		means: "The probe could not be sent, so this says nothing about the host. On most " +
			"systems that means the process may not open an ICMP socket.",
		do: "Nothing — the TCP connect answers the reachability question without needing " +
			"any privilege."},

	// --- the way out ---
	{check: CheckRoute, verdict: OK,
		means: "The machine has a route to this target and knows which interface will carry " +
			"it. That rules out a split tunnel or a missing route as the cause of anything " +
			"that fails below."},
	{check: CheckRoute, verdict: Warn, reason: ReasonPartialRoute,
		means: "Some of the addresses this name resolves to have no route out of this " +
			"machine. That is usually IPv6 on a network that carries none, and it makes a " +
			"client that prefers IPv6 hang before it falls back.",
		do: "Check whether this machine is meant to have IPv6. If it is not, the addresses " +
			"without a route are harmless; if it is, the missing route is the fault."},
	{check: CheckRoute, verdict: Unknown,
		means: "The routing table could not answer, so this says nothing about the target. " +
			"It is not a claim that no route exists.",
		do: "Read the TCP connect below — it answers whether the target responds without " +
			"needing this."},

	// --- the port ---
	{check: CheckTCP, verdict: OK,
		means: "The port is open and something is listening, which is the reachability " +
			"answer that matters — regardless of what ICMP said."},
	{check: CheckTCP, verdict: Fail,
		means: "Either nothing is listening on that port, or something between here and " +
			"there is dropping or refusing the connection. A refusal comes from the host; " +
			"a timeout usually comes from a firewall in the path.",
		do: "Confirm the service is running and bound to an address reachable from outside " +
			"the machine — a service bound to 127.0.0.1 answers locally and nowhere else. " +
			"Then trace the route to find where the connection stops."},

	// --- TLS ---
	{check: CheckTLSHandshake, verdict: OK,
		means: "The server completed a TLS handshake and presented a certificate, so the " +
			"four checks below have something real to inspect."},
	{check: CheckTLSHandshake, verdict: Fail,
		means: "The port speaks TLS but the handshake did not complete. The usual causes are " +
			"a protocol version or cipher suite neither side shares, or a server demanding a " +
			"client certificate.",
		do: "Check what versions and ciphers the server is configured to accept. A server " +
			"restricted to TLS 1.3 with an old client, or the reverse, fails exactly here."},
	{check: CheckTLSHandshake, verdict: Unknown,
		means: "The handshake completed but no certificate came back, which should not happen " +
			"on a normal server and leaves nothing to verify.",
		do: "Check whether something other than the service you expect is answering on this port."},

	{check: CheckTLSChain, verdict: OK,
		means: "The certificate chains to a root your machine trusts, using the intermediates " +
			"the server sent. A client with the same trust store accepts this connection."},
	{check: CheckTLSChain, verdict: Warn, reason: ReasonChainIncomplete,
		means: "It verified here only because your machine already held what was missing. " +
			"A client that does not — a container with a minimal trust store, a mobile device, " +
			"an older system — will reject this certificate. This is the classic bug that works " +
			"on the developer's laptop and fails in production.",
		do: "Configure the server to send its full chain: the leaf followed by every " +
			"intermediate, in order, root excluded."},
	{check: CheckTLSChain, verdict: Fail, reason: ReasonNoIntermediates,
		means: "The server sent only its own certificate and nothing linking it to a trusted " +
			"root, so the chain cannot be built. Clients reject it.",
		do: "Deploy the full chain file rather than the certificate alone — most providers " +
			"ship it as fullchain.pem or a bundle beside the certificate."},
	{check: CheckTLSChain, verdict: Fail, reason: ReasonSelfSigned,
		means: "The certificate vouches for itself, so it proves nothing about who is on the " +
			"other end. That is expected on a development server and never acceptable in front " +
			"of anything real.",
		do: "For a real service, issue a certificate from a CA your clients trust. For a " +
			"deliberate internal one, distribute the CA to the machines that must accept it — " +
			"never disable verification client-side."},
	{check: CheckTLSChain, verdict: Fail, reason: ReasonUntrustedRoot,
		means: "The chain is complete but ends at a root your machine does not trust. Either " +
			"the issuer is a private CA you have not installed, or something is intercepting " +
			"and re-signing the connection — a corporate proxy does exactly this.",
		do: "Look at the issuer above. If you recognize it as your organization's CA, install " +
			"it in the trust store. If you do not recognize it at all, find out what is sitting " +
			"in the middle of this connection before sending anything over it."},

	{check: CheckTLSHostname, verdict: OK,
		means: "The certificate names this host, so clients accept it for the address you used."},
	{check: CheckTLSHostname, verdict: Fail,
		means: "The certificate is for a different name. Every client verifying properly " +
			"refuses the connection, whether or not the chain itself is sound.",
		do: "Compare the names listed above with the one you dialled. Reaching a host by an " +
			"alias or an IP address that is not in the certificate causes this as often as a " +
			"genuinely wrong certificate does."},

	{check: CheckTLSExpiry, verdict: OK,
		means: "The certificate is valid, with enough time left that renewal is not urgent."},
	{check: CheckTLSExpiry, verdict: Warn,
		means: "Inside the renewal window. Nothing is broken yet, and everything breaks at " +
			"once when it lapses — an expiry outage takes the whole service down without warning.",
		do: "Confirm automatic renewal is actually running. A renewal that succeeds but never " +
			"reloads the server is the common failure, and it looks fine until the day it does not."},
	{check: CheckTLSExpiry, verdict: Fail, reason: ReasonExpired,
		means: "Clients are refusing this connection right now. This is an outage, not a warning.",
		do: "Renew and reload the service. If renewal is automated, find out why it stopped — " +
			"the certificate on disk may already be current while the running process still " +
			"holds the old one."},
	{check: CheckTLSExpiry, verdict: Fail, reason: ReasonNotYetValid,
		means: "The certificate is dated in the future, so clients reject it as firmly as an " +
			"expired one. Nearly always a clock problem rather than a certificate problem.",
		do: "Check the clock on this machine and on the server. A skewed clock makes valid " +
			"certificates look invalid across everything, not just here."},

	{check: CheckTLSVersion, verdict: OK,
		means: "A current protocol version was negotiated."},
	{check: CheckTLSVersion, verdict: Warn,
		means: "TLS 1.0 and 1.1 have known weaknesses, are refused by default in current " +
			"browsers and runtimes, and fail most compliance baselines. That the connection " +
			"worked here says only that this client still allows them.",
		do: "Configure the server for TLS 1.2 as a minimum, and 1.3 where the clients support it."},

	// --- HTTP ---
	{check: CheckHTTP, verdict: OK,
		means: "The service answered, so the path from here to the application is complete — " +
			"not merely to the port."},
	{check: CheckHTTP, verdict: Warn,
		means: "The service is running and answered; it just declined this particular request. " +
			"A 401 or 403 to an unauthenticated HEAD is a healthy endpoint doing its job, and a " +
			"404 usually means the path rather than the host is wrong.",
		do: "Nothing, if you only wanted to know the service is up. Check the path or your " +
			"credentials if you expected content."},
	{check: CheckHTTP, verdict: Fail,
		means: "The port is open but the application behind it is failing or unreachable — " +
			"a proxy with no healthy backend answers exactly like this.",
		do: "Read the service's own logs. The network reached it, so the problem is on the " +
			"far side rather than in the path."},
}
