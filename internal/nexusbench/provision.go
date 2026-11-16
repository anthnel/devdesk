package nexusbench

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// The REST paths and names the provisioning uses. Every payload shape below is
// a hypothesis (H-REST) taken from the Sonatype documentation, to be checked
// against the instance's /service/rest/swagger.json at step 10.
const (
	restAPI         = "/service/rest/v1"
	swaggerPath     = "/service/rest/swagger.json"
	restStatusPath  = restAPI + "/status"
	eulaPath        = restAPI + "/system/eula"
	blobStoresPath  = restAPI + "/blobstores"
	activeRealms    = restAPI + "/security/realms/active"
	anonymousPath   = restAPI + "/security/anonymous"
	rolesPath       = restAPI + "/security/roles"
	usersPath       = restAPI + "/security/users"
	dockerReposPath = restAPI + "/repositories/docker"

	adminUserID   = "admin"
	anonymousUser = "anonymous"
	pullUserID    = "bench-pull"
	pullRoleID    = "bench-docker-pull"

	defaultBlobStore       = "default"
	defaultAnonymousRealm  = "NexusAuthorizingRealm"
	dockerHostedRepository = "docker-hosted"
	dockerProxyRepository  = "docker-proxy"

	// generatedPasswordLength and generatedPasswordAlphabet describe the
	// passwords the bench makes up: 32 characters of [A-Za-z0-9], about 190 bits.
	generatedPasswordLength   = 32
	generatedPasswordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	// passwordRejectFrom is the first byte value that would bias the choice of a
	// character: 248 is the largest multiple of 62 below 256.
	passwordRejectFrom = 248

	// repositoryMaxAgeMinutes is the cache age of the proxy repository (one day).
	repositoryMaxAgeMinutes = 1440
)

// requiredRealms are the realms that have to be active, in the order they are
// added when missing. DockerToken is the Docker Bearer Token Realm, which
// anonymous Docker pulls need.
var requiredRealms = []string{"NexusAuthenticatingRealm", "DockerToken"}

// pullPrivileges are the privileges of the pull role: read-only on every Docker
// repository.
var pullPrivileges = []string{"nx-repository-view-docker-*-browse", "nx-repository-view-docker-*-read"}

// InitialPassword reads the password a fresh Nexus generated for its admin
// account (/nexus-data/admin.password). ok is false, without an error, when the
// file does not exist, which is the normal state once the password has been
// changed.
type InitialPassword interface {
	Read(ctx context.Context) (password string, ok bool, err error)
}

// Provision brings a running Nexus to the state the bench needs and returns
// what it read from the instance. It is idempotent: every object is read, then
// created when absent or replaced when present, so a second run changes nothing
// and a run after a hand edit puts the bench back. baseURL is the REST origin
// (scheme, host and port, no path).
//
// The order is: version, admin credentials, EULA, blob store, realms, anonymous
// access, pull role, pull user, then the hosted, proxy and group repositories.
// A password is written to the store before the instance is told about it, so an
// interruption never loses one. Nothing Provision writes to out or returns in an
// error contains a password.
func Provision(ctx context.Context, baseURL string, ip InitialPassword, store SecretStore, p Params, out io.Writer) (ProvisionResult, error) {
	if out == nil {
		out = io.Discard
	}
	pv := &provisioner{base: baseURL, ip: ip, store: store, p: p, out: out}
	defer func() {
		if pv.api != nil {
			pv.api.closeIdle()
		}
	}()
	res, err := pv.run(ctx)
	if err != nil {
		return res, asBenchError(err, p, baseURL)
	}
	return res, nil
}

// provisioner holds one run.
type provisioner struct {
	base  string
	ip    InitialPassword
	store SecretStore
	p     Params
	out   io.Writer

	// secrets mirrors what the store holds; every Save goes through saveSecrets.
	secrets Secrets
	// api is the client authenticated as admin, once the credentials are settled.
	api *client
}

func (pv *provisioner) run(ctx context.Context) (ProvisionResult, error) {
	var res ProvisionResult
	anonymous := newClient(pv.base, "", "")
	defer anonymous.closeIdle()
	var err error
	if res.NexusVersion, res.Edition, err = pv.readVersion(ctx, anonymous); err != nil {
		return res, err
	}
	if pv.api, err = pv.adminClient(ctx); err != nil {
		return res, err
	}
	if res.EULAAccepted, err = pv.ensureEULA(ctx); err != nil {
		return res, err
	}
	for _, step := range []func(context.Context) error{
		pv.requireBlobStore, pv.ensureRealms, pv.ensureAnonymous, pv.ensurePullRole, pv.ensurePullUser, pv.ensureRepositories,
	} {
		if err := step(ctx); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (pv *provisioner) say(format string, args ...any) {
	_, _ = fmt.Fprintf(pv.out, "provision: "+format+"\n", args...)
}

// fail builds the BenchError of a step that is not a REST failure.
func (pv *provisioner) fail(cause, check string, err error) error {
	return &BenchError{Kind: ProvisionFailed, Cause: cause, Check: check, Err: err}
}

// podsCommand is the kubectl command that shows the Nexus pod.
func podsCommand(p Params) string {
	return fmt.Sprintf("kubectl --context %s -n %s get pods -l app.kubernetes.io/name=nexus", p.Context, p.Namespace)
}

// ---- version ------------------------------------------------------------------

// readVersion reads the version and the edition from the Server header of the
// status call ("Nexus/3.98.1-01 (COMMUNITY)"), falling back to the version of
// the swagger document. An instance that gives neither has an empty version.
func (pv *provisioner) readVersion(ctx context.Context, c *client) (version, edition string, err error) {
	resp, err := c.expect(ctx, request{op: "read the instance status", method: http.MethodGet, path: restStatusPath})
	if err != nil {
		return "", "", err
	}
	if v, e, ok := parseServerHeader(resp.header.Get("Server")); ok {
		pv.say("Nexus %s (%s)", v, e)
		return v, e, nil
	}
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if _, err := c.get(ctx, "read the API description", swaggerPath, nil, &doc); err != nil {
		// The version is optional: a refusal or an answer that is not JSON
		// (a proxy's page) leaves it empty, a transport failure does not.
		var api *APIError
		var dec *decodeError
		if !errors.As(err, &api) && !errors.As(err, &dec) {
			return "", "", err
		}
	}
	if doc.Info.Version == "" {
		pv.say("the Nexus version could not be read")
		return "", "", nil
	}
	pv.say("Nexus %s", doc.Info.Version)
	return doc.Info.Version, "", nil
}

// parseServerHeader reads "Nexus/<version> (<EDITION>)".
func parseServerHeader(header string) (version, edition string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(header), "Nexus/")
	if !found {
		return "", "", false
	}
	version, tail, _ := strings.Cut(rest, " ")
	if version == "" {
		return "", "", false
	}
	edition = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(tail), "("), ")")
	return version, edition, true
}

// ---- admin credentials -----------------------------------------------------------

// adminClient settles which password the admin account has and returns a client
// that uses it. The candidates are the stored admin password, then the pending
// one (a change that was interrupted); when neither is accepted the initial
// password of the pod is used, and the account gets a generated one. An empty
// password is never a candidate: "{}" in the secrets file means "not provided".
func (pv *provisioner) adminClient(ctx context.Context) (*client, error) {
	sec, _, err := pv.store.Load()
	if err != nil {
		return nil, pv.fail("the secrets file could not be read", "check the file named by NEXUS_BENCH_SECRETS, or delete it to start again", err)
	}
	pv.secrets = sec
	candidates := []string{sec.AdminPassword, sec.PendingAdminPassword}
	for i, password := range candidates {
		if password == "" || (i == 1 && password == candidates[0]) {
			continue
		}
		accepted, err := pv.adminAccepts(ctx, password)
		if err != nil {
			return nil, err
		}
		if !accepted {
			continue
		}
		if i == 1 {
			pv.secrets.AdminPassword, pv.secrets.PendingAdminPassword = password, ""
			if err := pv.saveSecrets("the pending admin password is in effect but could not be promoted in the secrets file"); err != nil {
				return nil, err
			}
			pv.say("the admin password of an interrupted change is now the current one")
		} else {
			pv.say("the stored admin password is accepted")
		}
		return newClient(pv.base, adminUserID, password), nil
	}
	return pv.firstLogin(ctx)
}

// adminAccepts reports whether the instance accepts password for admin. A 401 is
// a refusal; any other failure is an error.
func (pv *provisioner) adminAccepts(ctx context.Context, password string) (bool, error) {
	c := newClient(pv.base, adminUserID, password)
	defer c.closeIdle()
	resp, err := c.do(ctx, request{
		op: "check the admin account", method: http.MethodGet, path: usersPath, query: url.Values{"userId": {adminUserID}},
	})
	if err != nil {
		return false, err
	}
	switch {
	case resp.status == http.StatusUnauthorized:
		return false, nil
	case resp.status < 200 || resp.status > 299:
		return false, &APIError{Op: "check the admin account", Status: resp.status}
	}
	return true, nil
}

// firstLogin changes the initial admin password of the pod. The new password is
// stored as pending before it is sent, and promoted once the instance has it.
func (pv *provisioner) firstLogin(ctx context.Context) (*client, error) {
	initial, ok, err := pv.ip.Read(ctx)
	if err != nil {
		return nil, pv.fail("the initial admin password could not be read from the pod", "check that the pod is running: "+podsCommand(pv.p), err)
	}
	if !ok || initial == "" {
		return nil, pv.adminUnknown()
	}
	accepted, err := pv.adminAccepts(ctx, initial)
	if err != nil {
		return nil, err
	}
	if !accepted {
		return nil, pv.adminUnknown()
	}

	next := pv.secrets.PendingAdminPassword
	if next == "" {
		if next, err = generatePassword(); err != nil {
			return nil, pv.fail("a password could not be generated", "check the operating system's random source", err)
		}
		pv.secrets.PendingAdminPassword = next
		if err := pv.saveSecrets("the new admin password could not be stored, so the admin password was not changed"); err != nil {
			return nil, err
		}
	}
	first := newClient(pv.base, adminUserID, initial)
	defer first.closeIdle()
	if err := changePassword(ctx, first, adminUserID, next); err != nil {
		return nil, err
	}
	pv.secrets.AdminPassword, pv.secrets.PendingAdminPassword = next, ""
	if err := pv.saveSecrets("the admin password was changed but the secrets file could not be updated; it still holds the new password as pending"); err != nil {
		return nil, err
	}
	pv.say("the admin password was changed")
	return newClient(pv.base, adminUserID, next), nil
}

func (pv *provisioner) adminUnknown() error {
	return &BenchError{
		Kind:  ProvisionFailed,
		Cause: "the bench admin password is unknown: the pod has no admin.password file and the secrets file holds no password the instance accepts",
		Check: "run `mise run nexus-bench:down`, then `mise run nexus-bench:up`; this deletes the bench's data and starts a fresh instance",
	}
}

// saveSecrets stores pv.secrets, wrapping a failure with cause.
func (pv *provisioner) saveSecrets(cause string) error {
	if err := pv.store.Save(pv.secrets); err != nil {
		return pv.fail(cause, "check the secrets file path, its folder permissions and the free disk space", err)
	}
	return nil
}

// changePassword sets the password of a user; the body is the plain password.
func changePassword(ctx context.Context, c *client, userID, password string) error {
	_, err := c.expect(ctx, request{
		op: "change the password of " + userID, method: http.MethodPut,
		path: usersPath + "/" + url.PathEscape(userID) + "/change-password", contentType: "text/plain", body: []byte(password),
	})
	return err
}

// generatePassword returns a password of generatedPasswordLength characters of
// [A-Za-z0-9] from crypto/rand, with no modulo bias.
func generatePassword() (string, error) {
	out := make([]byte, 0, generatedPasswordLength)
	buf := make([]byte, 2*generatedPasswordLength)
	for len(out) < generatedPasswordLength {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < passwordRejectFrom && len(out) < generatedPasswordLength {
				out = append(out, generatedPasswordAlphabet[int(b)%len(generatedPasswordAlphabet)])
			}
		}
	}
	return string(out), nil
}

// ---- EULA --------------------------------------------------------------------------

// ensureEULA reports whether the EULA is accepted at the end. It accepts it only
// when the operator allowed it, sending the disclaimer exactly as the instance
// served it; otherwise it stops, before anything is created.
func (pv *provisioner) ensureEULA(ctx context.Context) (bool, error) {
	var eula struct {
		Accepted   bool            `json:"accepted"`
		Disclaimer json.RawMessage `json:"disclaimer"`
	}
	found, err := pv.api.get(ctx, "read the EULA", eulaPath, nil, &eula)
	if err != nil {
		return false, err
	}
	if !found {
		pv.say("this instance has no EULA endpoint")
		return false, nil
	}
	if eula.Accepted {
		pv.say("the EULA is already accepted")
		return true, nil
	}
	if !pv.p.AcceptEULA {
		return false, &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the Nexus EULA is not accepted and " + envAcceptEULA + " is not set to yes, so no repository, role or account was created",
			Check: "read the EULA in the Nexus UI, then run again with " + envAcceptEULA + "=yes if you accept it",
		}
	}
	payload := struct {
		Accepted   bool            `json:"accepted"`
		Disclaimer json.RawMessage `json:"disclaimer"`
	}{true, eula.Disclaimer}
	if err := pv.api.write(ctx, "accept the EULA", http.MethodPost, eulaPath, payload); err != nil {
		return false, err
	}
	pv.say("the EULA was accepted because %s=yes", envAcceptEULA)
	return true, nil
}

// ---- blob store, realms, anonymous access -----------------------------------------------

// requireBlobStore checks that the default blob store exists: the repositories
// are created on it.
func (pv *provisioner) requireBlobStore(ctx context.Context) error {
	var stores []struct {
		Name string `json:"name"`
	}
	if err := pv.api.read(ctx, "list the blob stores", blobStoresPath, nil, &stores); err != nil {
		return err
	}
	for _, s := range stores {
		if s.Name == defaultBlobStore {
			return nil
		}
	}
	return pv.fail("the blob store "+defaultBlobStore+" does not exist on the instance, so the repositories cannot be created",
		"create a blob store named "+defaultBlobStore+" in the Nexus UI, or run `mise run nexus-bench:down` then `mise run nexus-bench:up`", nil)
}

// ensureRealms adds the missing required realms to the active ones, keeping the
// others: replacing the list would drop a realm the instance needs.
func (pv *provisioner) ensureRealms(ctx context.Context) error {
	var active []string
	if err := pv.api.read(ctx, "read the active realms", activeRealms, nil, &active); err != nil {
		return err
	}
	want := slices.Clone(active)
	for _, realm := range requiredRealms {
		if !slices.Contains(want, realm) {
			want = append(want, realm)
		}
	}
	if len(want) == len(active) {
		pv.say("the realms are in place")
		return nil
	}
	if err := pv.api.write(ctx, "set the active realms", http.MethodPut, activeRealms, want); err != nil {
		return err
	}
	pv.say("the realms were set")
	return nil
}

// ensureAnonymous sets whether anonymous access is enabled, keeping the realm of
// the anonymous user as the instance has it.
func (pv *provisioner) ensureAnonymous(ctx context.Context) error {
	var current struct {
		RealmName string `json:"realmName"`
	}
	if err := pv.api.read(ctx, "read the anonymous access", anonymousPath, nil, &current); err != nil {
		return err
	}
	if current.RealmName == "" {
		current.RealmName = defaultAnonymousRealm
	}
	payload := map[string]any{"enabled": pv.p.Anonymous, "userId": anonymousUser, "realmName": current.RealmName}
	if err := pv.api.write(ctx, "set the anonymous access", http.MethodPut, anonymousPath, payload); err != nil {
		return err
	}
	pv.say("anonymous access enabled: %t", pv.p.Anonymous)
	return nil
}

// ---- role and user ----------------------------------------------------------------------

func (pv *provisioner) ensurePullRole(ctx context.Context) error {
	role := map[string]any{
		"id": pullRoleID, "name": pullRoleID, "description": "Read-only access to the Docker repositories of the bench",
		"privileges": pullPrivileges, "roles": []string{},
	}
	found, err := pv.api.get(ctx, "read role "+pullRoleID, rolesPath+"/"+pullRoleID, nil, nil)
	if err != nil {
		return err
	}
	if !found {
		if err := pv.api.write(ctx, "create role "+pullRoleID, http.MethodPost, rolesPath, role); err != nil {
			return err
		}
		pv.say("role %s created", pullRoleID)
		return nil
	}
	if err := pv.api.write(ctx, "update role "+pullRoleID, http.MethodPut, rolesPath+"/"+pullRoleID, role); err != nil {
		return err
	}
	pv.say("role %s updated", pullRoleID)
	return nil
}

// userRef is the part of a user the provisioning reads back.
type userRef struct {
	UserID string `json:"userId"`
}

// ensurePullUser creates or replaces the read-only user. Its password is stored
// before the instance gets it. The list call filters by substring, so the
// element of the exact id is the one that counts.
func (pv *provisioner) ensurePullUser(ctx context.Context) error {
	var users []userRef
	op := "read user " + pullUserID
	if err := pv.api.read(ctx, op, usersPath, url.Values{"userId": {pullUserID}}, &users); err != nil {
		return err
	}
	exists := slices.ContainsFunc(users, func(u userRef) bool { return u.UserID == pullUserID })

	password := pv.secrets.PullPassword
	if password == "" {
		var err error
		if password, err = generatePassword(); err != nil {
			return pv.fail("a password could not be generated", "check the operating system's random source", err)
		}
		pv.secrets.PullPassword = password
		if err := pv.saveSecrets("the password of " + pullUserID + " could not be stored, so the user was not changed"); err != nil {
			return err
		}
	}
	user := map[string]any{
		"userId": pullUserID, "firstName": "Bench", "lastName": "Pull", "emailAddress": "bench-pull@bench.invalid",
		"status": "active", "roles": []string{pullRoleID},
	}
	if !exists {
		user["password"] = password
		if err := pv.api.write(ctx, "create user "+pullUserID, http.MethodPost, usersPath, user); err != nil {
			return err
		}
		pv.say("user %s created", pullUserID)
		return nil
	}
	user["source"] = "default"
	if err := pv.api.write(ctx, "update user "+pullUserID, http.MethodPut, usersPath+"/"+pullUserID, user); err != nil {
		return err
	}
	if err := changePassword(ctx, pv.api, pullUserID, password); err != nil {
		return err
	}
	pv.say("user %s updated", pullUserID)
	return nil
}

// ---- repositories -----------------------------------------------------------------------

type repositorySpec struct {
	kind, name string
	config     map[string]any
}

// repositories returns the three Docker repositories, in the order they can be
// created: the group needs its members.
func (pv *provisioner) repositories() []repositorySpec {
	storage := func(extra map[string]any) map[string]any {
		s := map[string]any{"blobStoreName": defaultBlobStore, "strictContentTypeValidation": true}
		for k, v := range extra {
			s[k] = v
		}
		return s
	}
	// Anonymous pulls need the Docker API without a forced Basic auth.
	docker := func(extra map[string]any) map[string]any {
		d := map[string]any{"v1Enabled": false, "forceBasicAuth": false}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	return []repositorySpec{
		{"hosted", dockerHostedRepository, map[string]any{
			"name": dockerHostedRepository, "online": true,
			"storage": storage(map[string]any{"writePolicy": "ALLOW"}),
			"docker":  docker(nil),
		}},
		{"proxy", dockerProxyRepository, map[string]any{
			"name": dockerProxyRepository, "online": true,
			"storage": storage(nil),
			"docker":  docker(nil),
			"proxy": map[string]any{
				"remoteUrl": pv.p.UpstreamURL, "contentMaxAge": repositoryMaxAgeMinutes, "metadataMaxAge": repositoryMaxAgeMinutes,
			},
			"dockerProxy":   map[string]any{"indexType": "REGISTRY"},
			"negativeCache": map[string]any{"enabled": true, "timeToLive": repositoryMaxAgeMinutes},
			"httpClient":    map[string]any{"blocked": false, "autoBlock": true},
		}},
		{"group", dockerGroupRepository, map[string]any{
			"name": dockerGroupRepository, "online": true,
			"storage": storage(nil),
			"docker":  docker(map[string]any{"httpPort": dockerContainerPort}),
			"group":   map[string]any{"memberNames": []string{dockerHostedRepository, dockerProxyRepository}},
		}},
	}
}

func (pv *provisioner) ensureRepositories(ctx context.Context) error {
	for _, repo := range pv.repositories() {
		collection := dockerReposPath + "/" + repo.kind
		found, err := pv.api.get(ctx, fmt.Sprintf("read the %s repository %s", repo.kind, repo.name), collection+"/"+repo.name, nil, nil)
		if err != nil {
			return err
		}
		if !found {
			op := fmt.Sprintf("create the %s repository %s", repo.kind, repo.name)
			if err := pv.api.write(ctx, op, http.MethodPost, collection, repo.config); err != nil {
				return err
			}
			pv.say("%s repository %s created", repo.kind, repo.name)
			continue
		}
		op := fmt.Sprintf("update the %s repository %s", repo.kind, repo.name)
		if err := pv.api.write(ctx, op, http.MethodPut, collection+"/"+repo.name, repo.config); err != nil {
			return err
		}
		pv.say("%s repository %s updated", repo.kind, repo.name)
	}
	return nil
}

// ---- errors -----------------------------------------------------------------------------

// asBenchError turns what the client and the steps returned into the BenchError
// of D-F: RESTUnreachable for a call that got no answer, ProvisionFailed for the
// rest. A BenchError passes unchanged. The text it builds holds the operation and
// the status, never a URL, a body or a password; the original error stays in Err.
func asBenchError(err error, p Params, baseURL string) error {
	var be *BenchError
	if errors.As(err, &be) {
		return err
	}
	pods := podsCommand(p)
	logs := fmt.Sprintf("kubectl --context %s -n %s logs deploy/nexus", p.Context, p.Namespace)
	var (
		api       *APIError
		transport *transportError
		large     *tooLargeError
		decode    *decodeError
	)
	switch {
	case errors.As(err, &transport):
		return &BenchError{
			Kind:  RESTUnreachable,
			Cause: fmt.Sprintf("the Nexus REST API at %s did not answer (%s)", hostOf(baseURL), transport.Op),
			Check: "check that the pod is Ready (" + pods + ") and that the NodePort answers from this machine",
			Err:   err,
		}
	case errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden):
		return &BenchError{
			Kind:  ProvisionFailed,
			Cause: "Nexus refused the bench account: " + api.Error(),
			Check: "the admin password in the secrets file may not match the instance; run `mise run nexus-bench:down` then `mise run nexus-bench:up`",
			Err:   err,
		}
	case errors.As(err, &api):
		return &BenchError{Kind: ProvisionFailed, Cause: "Nexus refused a request: " + api.Error(), Check: "read the log of the pod: " + logs, Err: err}
	case errors.As(err, &large):
		return &BenchError{
			Kind: ProvisionFailed, Cause: large.Error(),
			Check: "check that " + hostOf(baseURL) + " is the Nexus REST API and not another service", Err: err,
		}
	case errors.As(err, &decode):
		return &BenchError{
			Kind: ProvisionFailed, Cause: decode.Error(),
			Check: "compare the answer with /service/rest/swagger.json of the instance (H-REST); the version may differ from 3.98.1", Err: err,
		}
	}
	return &BenchError{Kind: ProvisionFailed, Cause: "provisioning failed", Check: "read the log of the pod: " + logs, Err: err}
}

// hostOf returns host:port of baseURL, without any user information, for a message.
func hostOf(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "the instance"
	}
	return u.Host
}
