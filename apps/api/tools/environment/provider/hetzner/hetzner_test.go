package hetzner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider/hetzner"
)

// These tests reach a simulated Hetzner Cloud, which keeps its SSH keys,
// servers, primary IPs and actions in memory and answers as the reference
// of Hetzner's API says (https://docs.hetzner.cloud/reference/cloud). No
// test calls Hetzner itself: the first real turn-on does, by hand.

const (
	token     = "hetzner-token-of-the-simulation"
	deployKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK4m2tR5i7Wq9xD0aB3cE6fG8hJ1kL2mN3oP4qR5sT6u"
	created   = "2026-11-19T14:00:00Z"
)

// request is what one request carried.
type request struct {
	method        string
	path          string
	query         string
	authorization string
	body          map[string]any
}

// simulatedHetzner is a stand-in for the API of Hetzner Cloud.
type simulatedHetzner struct {
	*httptest.Server

	mu       sync.Mutex
	requests []request
	nextID   int64
	keys     map[int64]map[string]any
	servers  map[int64]map[string]any
	// addresses are the primary IPs, by id, with the server they belong to.
	addresses map[int64]int64
	// keepsAddresses makes deleting a server keep its primary IP, as Hetzner
	// does when its auto_delete is off.
	keepsAddresses bool
	// refuseServers answers every creation of a server with this error.
	refuseServers *apiError
	// failStarts makes every start of a server end in an error.
	failStarts bool
	actions    map[int64]string
}

type apiError struct {
	status       int
	code, detail string
}

func newSimulatedHetzner(t *testing.T) *simulatedHetzner {
	t.Helper()
	simulated := &simulatedHetzner{
		nextID:    100,
		keys:      map[int64]map[string]any{},
		servers:   map[int64]map[string]any{},
		addresses: map[int64]int64{},
		actions:   map[int64]string{},
	}
	simulated.Server = httptest.NewServer(http.HandlerFunc(simulated.serve))
	t.Cleanup(simulated.Close)
	return simulated
}

func (s *simulatedHetzner) adapter() *hetzner.Provider {
	return hetzner.New(hetzner.Settings{
		Endpoint:     s.URL + "/v1",
		Token:        config.NewSecret(token),
		Labels:       map[string]string{"environment": "staging"},
		PollInterval: time.Millisecond,
	})
}

func (s *simulatedHetzner) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	s.requests = append(s.requests, request{
		method: r.Method, path: path, query: r.URL.RawQuery,
		authorization: r.Header.Get("Authorization"), body: body,
	})
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+token {
		answerError(w, apiError{http.StatusUnauthorized, "unauthorized", "unable to authenticate"})
		return
	}
	resource, id, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	switch r.Method + " " + resource {
	case "POST ssh_keys":
		s.createKey(w, body)
	case "GET ssh_keys":
		s.list(w, "ssh_keys", s.keys, r)
	case "DELETE ssh_keys":
		s.deleteKey(w, id)
	case "POST servers":
		s.createServer(w, body)
	case "GET servers":
		s.getServers(w, r, id)
	case "DELETE servers":
		s.deleteServer(w, id)
	case "GET actions":
		s.listActions(w, r)
	case "GET primary_ips":
		s.getAddress(w, id)
	case "DELETE primary_ips":
		s.deleteAddress(w, id)
	default:
		answerError(w, apiError{http.StatusNotFound, "not_found", "no such route"})
	}
}

func answer(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func answerError(w http.ResponseWriter, failure apiError) {
	answer(w, failure.status, map[string]any{"error": map[string]any{"code": failure.code, "message": failure.detail, "details": map[string]any{}}})
}

func (s *simulatedHetzner) id() int64 {
	s.nextID++
	return s.nextID
}

// action starts an action, which is running until it is read.
func (s *simulatedHetzner) action(command string, resource int64, kind string) map[string]any {
	id := s.id()
	s.actions[id] = command
	return map[string]any{
		"id": id, "command": command, "status": "running", "progress": 0, "started": created,
		"finished": nil, "error": nil, "resources": []any{map[string]any{"id": resource, "type": kind}},
	}
}

func named(resources map[int64]map[string]any, name any) bool {
	return slices.ContainsFunc(slices.Collect(maps.Values(resources)), func(resource map[string]any) bool { return resource["name"] == name })
}

func (s *simulatedHetzner) createKey(w http.ResponseWriter, body map[string]any) {
	if named(s.keys, body["name"]) {
		answerError(w, apiError{http.StatusConflict, "uniqueness_error", "SSH key with the same name already exists"})
		return
	}
	id := s.id()
	key := map[string]any{
		"id": id, "name": body["name"], "public_key": body["public_key"], "labels": body["labels"],
		"fingerprint": "b7:2f:30:a0:2f:6c:58:6c:21:04:58:61:ba:06:3b:2f", "created": created,
	}
	s.keys[id] = key
	answer(w, http.StatusCreated, map[string]any{"ssh_key": key})
}

func (s *simulatedHetzner) deleteKey(w http.ResponseWriter, id string) {
	number, _ := strconv.ParseInt(id, 10, 64)
	if _, found := s.keys[number]; !found {
		answerError(w, apiError{http.StatusNotFound, "not_found", "SSH key not found"})
		return
	}
	delete(s.keys, number)
	w.WriteHeader(http.StatusNoContent)
}

func (s *simulatedHetzner) createServer(w http.ResponseWriter, body map[string]any) {
	if s.refuseServers != nil {
		answerError(w, *s.refuseServers)
		return
	}
	if named(s.servers, body["name"]) {
		answerError(w, apiError{http.StatusConflict, "uniqueness_error", "server name is already used"})
		return
	}
	id, address := s.id(), s.id()
	s.addresses[address] = id
	server := map[string]any{
		"id": id, "name": body["name"], "status": "initializing", "created": created,
		"public_net": map[string]any{
			"ipv4":         map[string]any{"id": address, "ip": "198.51.100.7", "blocked": false, "dns_ptr": "static.7.100.51.198.clients.your-server.de"},
			"ipv6":         nil,
			"floating_ips": []any{},
			"firewalls":    []any{},
		},
		"private_net": []any{},
		"server_type": map[string]any{"id": 114, "name": body["server_type"]},
		"location":    map[string]any{"id": 1, "name": body["location"]},
		"image":       map[string]any{"id": 387894169, "type": "system", "name": body["image"]},
		"iso":         nil, "rescue_enabled": false, "locked": false, "backup_window": nil,
		"outgoing_traffic": 0, "ingoing_traffic": 0, "included_traffic": 21990232555520,
		"protection": map[string]any{"delete": false, "rebuild": false},
		"labels":     body["labels"], "primary_disk_size": 40, "volumes": []any{}, "load_balancers": []any{},
	}
	s.servers[id] = server
	answer(w, http.StatusCreated, map[string]any{
		"server":        server,
		"action":        s.action("create_server", id, "server"),
		"next_actions":  []any{s.action("start_server", id, "server")},
		"root_password": nil,
	})
	server["status"] = "running"
}

func (s *simulatedHetzner) getServers(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		s.list(w, "servers", s.servers, r)
		return
	}
	number, _ := strconv.ParseInt(id, 10, 64)
	server, found := s.servers[number]
	if !found {
		answerError(w, apiError{http.StatusNotFound, "not_found", "server not found"})
		return
	}
	answer(w, http.StatusOK, map[string]any{"server": server})
}

func (s *simulatedHetzner) deleteServer(w http.ResponseWriter, id string) {
	number, _ := strconv.ParseInt(id, 10, 64)
	if _, found := s.servers[number]; !found {
		answerError(w, apiError{http.StatusNotFound, "not_found", "server not found"})
		return
	}
	delete(s.servers, number)
	if !s.keepsAddresses {
		maps.DeleteFunc(s.addresses, func(_, server int64) bool { return server == number })
	}
	answer(w, http.StatusOK, map[string]any{"action": s.action("delete_server", number, "server")})
}

func (s *simulatedHetzner) getAddress(w http.ResponseWriter, id string) {
	number, _ := strconv.ParseInt(id, 10, 64)
	if _, found := s.addresses[number]; !found {
		answerError(w, apiError{http.StatusNotFound, "not_found", "primary IP not found"})
		return
	}
	answer(w, http.StatusOK, map[string]any{"primary_ip": map[string]any{
		"id": number, "ip": "198.51.100.7", "type": "ipv4", "name": "primary_ip-" + id, "auto_delete": false,
		"assignee_id": nil, "assignee_type": "server", "blocked": false, "created": created, "labels": map[string]any{},
		"protection": map[string]any{"delete": false}, "dns_ptr": []any{}, "location": map[string]any{"id": 1, "name": "fsn1"},
	}})
}

func (s *simulatedHetzner) deleteAddress(w http.ResponseWriter, id string) {
	number, _ := strconv.ParseInt(id, 10, 64)
	delete(s.addresses, number)
	w.WriteHeader(http.StatusNoContent)
}

// list answers a list, filtered by name and by a label selector of
// key=value terms, in one page.
func (s *simulatedHetzner) list(w http.ResponseWriter, kind string, resources map[int64]map[string]any, r *http.Request) {
	query := r.URL.Query()
	found := []any{}
	for _, id := range slices.Sorted(maps.Keys(resources)) {
		found = append(found, matching(resources[id], query.Get("name"), query.Get("label_selector"))...)
	}
	answer(w, http.StatusOK, map[string]any{
		kind:   found,
		"meta": map[string]any{"pagination": map[string]any{"page": 1, "per_page": 50, "previous_page": nil, "next_page": nil, "last_page": 1, "total_entries": len(found)}},
	})
}

// matching gives the resource when it has the name, if one is asked for,
// and every label of the selector.
func matching(resource map[string]any, name, selector string) []any {
	if name != "" && resource["name"] != name {
		return nil
	}
	values, _ := resource["labels"].(map[string]any)
	missing := slices.ContainsFunc(strings.Split(selector, ","), func(term string) bool {
		key, value, _ := strings.Cut(term, "=")
		return term != "" && values[key] != value
	})
	if missing {
		return nil
	}
	return []any{resource}
}

// listActions answers the actions asked for by id, each finished now.
func (s *simulatedHetzner) listActions(w http.ResponseWriter, r *http.Request) {
	found := []any{}
	for _, id := range r.URL.Query()["id"] {
		found = append(found, s.finished(id)...)
	}
	answer(w, http.StatusOK, map[string]any{
		"actions": found,
		"meta":    map[string]any{"pagination": map[string]any{"page": 1, "per_page": 50, "previous_page": nil, "next_page": nil, "last_page": 1, "total_entries": len(found)}},
	})
}

// finished gives the action with that id, which has ended now: in success,
// or in an error for the start of a server when the test asks for it.
func (s *simulatedHetzner) finished(id string) []any {
	number, _ := strconv.ParseInt(id, 10, 64)
	command, known := s.actions[number]
	if !known {
		return nil
	}
	action := map[string]any{"id": number, "command": command, "status": "success", "progress": 100, "started": created, "finished": created, "error": nil, "resources": []any{}}
	if s.failStarts && command == "start_server" {
		action["status"] = "error"
		action["error"] = map[string]any{"code": "action_failed", "message": "Action failed"}
	}
	return []any{action}
}

// sent gives the requests of that method and path.
func (s *simulatedHetzner) sent(method, path string) []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(s.requests), func(each request) bool { return each.method != method || each.path != path })
}

func (s *simulatedHetzner) counts() (keys, servers, addresses int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys), len(s.servers), len(s.addresses)
}

func aSpecification() provider.Specification {
	return provider.Specification{
		Name:            "staging",
		Size:            "cx23",
		Region:          "fsn1",
		SystemImage:     "ubuntu-26.04",
		CloudInit:       config.NewSecret("#cloud-config\nssh_pwauth: false\n"),
		DeployPublicKey: deployKey,
		Labels:          map[string]string{"environment": "staging"},
	}
}

func TestCreateRegistersTheDeployKeyAndCreatesTheServerWithIt(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)

	server, err := simulated.adapter().Create(context.Background(), aSpecification())

	require.NoError(t, err)
	assert.Equal(t, provider.Server{
		Name: "staging", Address: netip.MustParseAddr("198.51.100.7"), Created: time.Date(2026, 11, 19, 14, 0, 0, 0, time.UTC),
	}, server)
	keys := simulated.sent(http.MethodPost, "/ssh_keys")
	require.Len(t, keys, 1)
	assert.Equal(t, "Bearer "+token, keys[0].authorization)
	assert.Equal(t, map[string]any{
		"name": "staging", "public_key": deployKey, "labels": map[string]any{"environment": "staging"},
	}, keys[0].body)
	servers := simulated.sent(http.MethodPost, "/servers")
	require.Len(t, servers, 1)
	assert.Equal(t, map[string]any{
		"name":               "staging",
		"server_type":        "cx23",
		"image":              "ubuntu-26.04",
		"location":           "fsn1",
		"ssh_keys":           []any{float64(101)},
		"user_data":          "#cloud-config\nssh_pwauth: false\n",
		"labels":             map[string]any{"environment": "staging"},
		"start_after_create": true,
		"public_net":         map[string]any{"enable_ipv4": true, "enable_ipv6": false},
	}, servers[0].body, "an ordinary IPv4 address, no IPv6, the deploy key so that no root password is e-mailed, and no firewall")
}

func TestCreateReturnsOnceHetznerSaysTheServerRuns(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)

	_, err := simulated.adapter().Create(context.Background(), aSpecification())

	require.NoError(t, err)
	polled := simulated.sent(http.MethodGet, "/actions")
	require.NotEmpty(t, polled)
	assert.Contains(t, polled[0].query, "id=104", "the creation of the server is waited for")
	assert.Contains(t, polled[0].query, "id=105", "and its start")
}

func TestAServerThatFailsToStartIsAnErrorAndStaysForTheCallerToDestroy(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	simulated.failStarts = true
	adapter := simulated.adapter()

	_, err := adapter.Create(context.Background(), aSpecification())

	require.ErrorContains(t, err, "wait until the server runs: Action failed")
	require.NoError(t, adapter.Destroy(context.Background(), "staging"))
}

func TestAServerThatCannotBeCreatedLeavesNoKeyBehind(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	simulated.refuseServers = &apiError{http.StatusUnprocessableEntity, "invalid_input", "image ubuntu-26.04 not found"}

	_, err := simulated.adapter().Create(context.Background(), aSpecification())

	require.ErrorContains(t, err, "create the server: image ubuntu-26.04 not found")
	keys, servers, addresses := simulated.counts()
	assert.Zero(t, keys)
	assert.Zero(t, servers)
	assert.Zero(t, addresses)
}

func TestANameInUseIsTheNameTaken(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	adapter := simulated.adapter()
	_, err := adapter.Create(context.Background(), aSpecification())
	require.NoError(t, err)

	_, err = adapter.Create(context.Background(), aSpecification())

	require.ErrorIs(t, err, provider.ErrNameTaken)
	keys, servers, _ := simulated.counts()
	assert.Equal(t, 1, keys, "the key of the server that exists is left as it is")
	assert.Equal(t, 1, servers)
}

func TestFindGivesTheServerWithTheNameAndTheLabelsOfTheEnvironment(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	adapter := simulated.adapter()
	_, err := adapter.Create(context.Background(), aSpecification())
	require.NoError(t, err)

	server, found, err := adapter.Find(context.Background(), "staging")

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, netip.MustParseAddr("198.51.100.7"), server.Address)
	lists := simulated.sent(http.MethodGet, "/servers")
	require.NotEmpty(t, lists)
	assert.Contains(t, lists[len(lists)-1].query, "label_selector=environment%3Dstaging")
	assert.Contains(t, lists[len(lists)-1].query, "name=staging")
}

func TestFindDoesNotGiveAServerOfTheNameWithoutTheLabels(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	specification := aSpecification()
	specification.Labels = map[string]string{"environment": "production"}
	_, err := simulated.adapter().Create(context.Background(), specification)
	require.NoError(t, err)

	_, found, err := simulated.adapter().Find(context.Background(), "staging")

	require.NoError(t, err)
	assert.False(t, found)
}

func TestDestroyLeavesNothingAtHetzner(t *testing.T) {
	t.Parallel()
	for _, keepsAddresses := range []bool{false, true} {
		t.Run(fmt.Sprintf("with the address deleted by Hetzner: %v", !keepsAddresses), func(t *testing.T) {
			t.Parallel()
			simulated := newSimulatedHetzner(t)
			simulated.keepsAddresses = keepsAddresses
			adapter := simulated.adapter()
			_, err := adapter.Create(context.Background(), aSpecification())
			require.NoError(t, err)

			require.NoError(t, adapter.Destroy(context.Background(), "staging"))

			keys, servers, addresses := simulated.counts()
			assert.Zero(t, keys, "the deploy key goes with the server")
			assert.Zero(t, servers)
			assert.Zero(t, addresses, "the address goes with the server")
			_, found, err := adapter.Find(context.Background(), "staging")
			require.NoError(t, err)
			assert.False(t, found)
		})
	}
}

func TestDestroyingWhatDoesNotExistSucceedsAndRemovesAKeyLeftBehind(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	adapter := simulated.adapter()
	simulated.mu.Lock()
	simulated.keys[1] = map[string]any{"id": 1, "name": "staging", "public_key": deployKey, "labels": map[string]any{"environment": "staging"}, "fingerprint": "x", "created": created}
	simulated.mu.Unlock()

	require.NoError(t, adapter.Destroy(context.Background(), "staging"))

	keys, _, _ := simulated.counts()
	assert.Zero(t, keys)
}

func TestAWrongTokenIsAnErrorThatDoesNotCarryTheToken(t *testing.T) {
	t.Parallel()
	simulated := newSimulatedHetzner(t)
	adapter := hetzner.New(hetzner.Settings{Endpoint: simulated.URL + "/v1", Token: config.NewSecret("a-revoked-token"), Labels: map[string]string{"environment": "staging"}})

	_, _, err := adapter.Find(context.Background(), "staging")

	require.ErrorContains(t, err, "unable to authenticate")
	assert.NotContains(t, err.Error(), "a-revoked-token")
}
