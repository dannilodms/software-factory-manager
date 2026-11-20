package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/database/databasetest"
)

// The API behind Caddy, as development runs it (Delivery and Health in
// CODING_STANDARDS.md). Each test starts a container of its own from the
// Caddy image of compose.yaml, with the Caddyfile of development and the
// rules of delivery it imports, in front of an API and a stand-in for Vite
// that listen on the machine. The image is built by `task services`, which
// `task api:test` runs first.

const (
	// caddyImage is the image of the Caddy service of compose.yaml.
	caddyImage = "software-factory-manager-caddy:development"
	// caddyfile is the Caddyfile of development. The test runs in the
	// directory of its package: the root of the repository is four up.
	caddyfile = "../../../../deploy/Caddyfile"
	// deliveryCaddyfile holds the rules of delivery, which the Caddyfile of
	// each environment imports from beside it.
	deliveryCaddyfile = "../../../../deploy/delivery.caddyfile"
	// theApp is what the stand-in for Vite answers to every request.
	theApp = "the app"
	// wrongSetupCode is a setup code that opens nothing.
	wrongSetupCode = `{"code":"ZZZZZ-ZZZZZ-ZZZZZ-ZZZZZ"}`
)

// site is Caddy serving the app and the API at one origin.
type site struct {
	url    string
	client *http.Client
	// container is the container of Caddy, whose output the tests read.
	container string
}

// reply is what the site answered.
type reply struct {
	received
	header http.Header
}

// machineHost is the interface where what Caddy reaches listens: the host of
// API_LISTEN_ADDRESS, as the API of development does. Docker Desktop reaches
// 127.0.0.1 of the machine; Docker Engine on Linux needs 0.0.0.0.
func machineHost(t *testing.T) string {
	t.Helper()
	address, found := os.LookupEnv("API_LISTEN_ADDRESS")
	require.True(t, found, "API_LISTEN_ADDRESS is not set: run the tests with `task api:test`, which reads .env.example")
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	return host
}

// onTheMachine gives the variables of an API on a temporary database, listening
// where Caddy reaches it.
func onTheMachine(t *testing.T, temporary databasetest.Database) map[string]string {
	t.Helper()
	environment := environmentOf(temporary)
	environment["API_LISTEN_ADDRESS"] = net.JoinHostPort(machineHost(t), "0")
	return environment
}

// docker runs a command of the Docker CLI and returns what it printed.
func docker(t *testing.T, arguments ...string) string {
	t.Helper()
	var errorOutput bytes.Buffer
	command := exec.CommandContext(t.Context(), "docker", arguments...)
	command.Stderr = &errorOutput
	printed, err := command.Output()
	require.NoError(t, err, "docker %s: %s", strings.Join(arguments, " "), errorOutput.String())
	return strings.TrimSpace(string(printed))
}

// standInForVite listens where Vite would and answers theApp to everything.
// It returns its port.
func standInForVite(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(machineHost(t), "0"))
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, theApp)
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	return port
}

// requireCaddyImage fails a test that needs the Caddy image when it is not
// built.
func requireCaddyImage(t *testing.T) {
	t.Helper()
	_, err := exec.CommandContext(t.Context(), "docker", "image", "inspect", caddyImage).Output()
	require.NoError(t, err, "the image %s is not built: run `task services`", caddyImage)
}

// mountOf mounts a file or a folder of the machine, read-only, at a path of
// the container. Docker Desktop creates a directory where what it mounts is
// missing, so it must exist.
func mountOf(t *testing.T, location, target string) string {
	t.Helper()
	absolute, err := filepath.Abs(location)
	require.NoError(t, err)
	_, err = os.Stat(absolute)
	require.NoError(t, err)
	return "type=bind,readonly,source=" + absolute + ",target=" + target
}

// behindCaddy starts Caddy in front of the API at apiURL and of a stand-in for
// Vite, and waits until it answers.
func behindCaddy(t *testing.T, apiURL string) site {
	t.Helper()
	requireCaddyImage(t)
	api, err := url.Parse(apiURL)
	require.NoError(t, err)

	container := docker(t, "run", "--detach", "--rm",
		"--label", "software-factory-manager.test=caddy",
		"--publish", "127.0.0.1::80",
		"--add-host", "host.docker.internal:host-gateway",
		"--mount", mountOf(t, caddyfile, "/etc/caddy/Caddyfile"),
		"--mount", mountOf(t, deliveryCaddyfile, "/etc/caddy/delivery.caddyfile"),
		"--env", "OTEL_TRACES_EXPORTER=none",
		"--env", "API_UPSTREAM=host.docker.internal:"+api.Port(),
		"--env", "WEB_UPSTREAM=host.docker.internal:"+standInForVite(t),
		caddyImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", container).Run()
	})

	published := strings.Fields(docker(t, "port", container, "80/tcp"))
	require.NotEmpty(t, published)
	caddy := site{
		container: container,
		url:       "http://" + published[0],
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	require.Eventually(t, func() bool {
		request, err := http.NewRequest(http.MethodGet, caddy.url+"/", nil)
		require.NoError(t, err)
		request.Host = "localhost"
		response, err := caddy.client.Do(request)
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return true
	}, 30*time.Second, 50*time.Millisecond, "Caddy did not answer: %s", docker(t, "logs", container))
	return caddy
}

// send sends a request to the site at the address a browser opens, and keeps
// the path as it is written, dot segments included.
func (s site) send(t *testing.T, method, path, body string) reply {
	t.Helper()
	request, err := http.NewRequest(method, s.url+path, strings.NewReader(body))
	require.NoError(t, err)
	request.Host = "localhost"
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	answer := reply{
		received: received{
			status:    response.StatusCode,
			mediaType: response.Header.Get("Content-Type"),
			allow:     response.Header.Get("Allow"),
			body:      string(raw),
		},
		header: response.Header,
	}
	_ = json.Unmarshal(raw, &answer.fields)
	return answer
}

// committedSchemas reads the schemas of the committed spec, the contract the
// app is generated from.
func committedSchemas(t *testing.T) huma.Registry {
	t.Helper()
	content, err := os.ReadFile(committedSpec)
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(content, &spec))
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	for name, schema := range spec.Components.Schemas {
		registry.Map()[name] = schemaOf(t, schema)
	}
	return registry
}

func schemaOf(t *testing.T, raw any) *huma.Schema {
	t.Helper()
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	schema := &huma.Schema{}
	require.NoError(t, json.Unmarshal(encoded, schema))
	return schema
}

// assertIsAProblem checks a body against the schema Problem of the committed
// spec.
func assertIsAProblem(t *testing.T, body string) {
	t.Helper()
	registry := committedSchemas(t)
	require.Contains(t, registry.Map(), "Problem")
	var value any
	require.NoError(t, json.Unmarshal([]byte(body), &value))
	result := &huma.ValidateResult{}

	huma.Validate(registry, registry.Map()["Problem"], huma.NewPathBuffer([]byte{}, 0), huma.ModeReadFromServer, value, result)

	assert.Empty(t, result.Errors, "the body does not follow the schema Problem of %s: %s", committedSpec, body)
}

func TestThroughCaddyTheAppAndTheAPIShareOneOrigin(t *testing.T) {
	t.Parallel()
	caddy := behindCaddy(t, startAPI(t, onTheMachine(t, databasetest.Migrated(t))))

	app := caddy.send(t, http.MethodGet, "/setup", "")
	api := caddy.send(t, http.MethodGet, "/api/setup/unknown", "")

	assert.Equal(t, http.StatusOK, app.status)
	assert.Equal(t, theApp, app.body)
	assert.Equal(t, http.StatusNotFound, api.status, api.body)
	assert.Equal(t, "application/problem+json", api.mediaType)
	assert.Equal(t, "not_found", api.fields["code"])
	assert.Equal(t, "no-store", api.header.Get("Cache-Control"))
}

func TestCaddyAnswers429InTheErrorFormatPastTheLimitOfTheSetup(t *testing.T) {
	t.Parallel()
	caddy := behindCaddy(t, startAPI(t, onTheMachine(t, databasetest.Migrated(t))))

	// Ten a minute pass untouched: the API answers each.
	for range 10 {
		within := caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode)
		requireSetupCodeInvalid(t, within.received)
		assert.Equal(t, "no-store", within.header.Get("Cache-Control"))
	}
	over := caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode)

	require.Equal(t, http.StatusTooManyRequests, over.status, over.body)
	assert.Equal(t, "application/problem+json", over.mediaType)
	assert.Equal(t, "no-store", over.header.Get("Cache-Control"))
	assert.NotEmpty(t, over.header.Get("Retry-After"))
	traceID, _ := over.fields["trace_id"].(string)
	assert.Regexp(t, `^[0-9a-f]{32}$`, traceID, "Caddy starts the trace of the request")
	assert.JSONEq(t, `{
		"title": "Too Many Requests",
		"status": 429,
		"code": "too_many_requests",
		"params": {},
		"trace_id": "`+traceID+`"
	}`, over.body)
	assertIsAProblem(t, over.body)
	// The POST operations of the setup share the limit.
	for _, path := range []string{"/api/setup/uploads", "/api/setup/conclude", "/api/setup/administrators"} {
		assert.Equal(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, path, wrongSetupCode).status, "POST %s", path)
	}
}

func TestCaddyAnswers429InTheErrorFormatPastTheLimitOfTheLoginCode(t *testing.T) {
	t.Parallel()
	caddy := behindCaddy(t, startAPI(t, onTheMachine(t, databasetest.Migrated(t))))

	// Ten a minute pass untouched: the API answers each. Each asks for an
	// address of its own, past which the API counts a limit of its own.
	for ask := range 10 {
		within := caddy.send(t, http.MethodPost, "/api/session/login_code", `{"email":"nobody+`+strconv.Itoa(ask)+`@fabrica.example"}`)
		require.Equal(t, http.StatusAccepted, within.status, within.body)
	}
	over := caddy.send(t, http.MethodPost, "/api/session/login_code", anEmailOfNoUser)

	require.Equal(t, http.StatusTooManyRequests, over.status, over.body)
	assert.Equal(t, "application/problem+json", over.mediaType)
	assert.Equal(t, "no-store", over.header.Get("Cache-Control"))
	assert.NotEmpty(t, over.header.Get("Retry-After"))
	assert.Equal(t, "too_many_requests", over.fields["code"])
	assertIsAProblem(t, over.body)
	// Checking a code shares the limit.
	assert.Equal(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, "/api/session/login_code/check", `{"code":"123456"}`).status)
	// The zone of the setup is apart: its first check reaches the API.
	requireSetupCodeInvalid(t, caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode).received)
}

// anEmailOfNoUser asks for a login code that no one receives.
const anEmailOfNoUser = `{"email":"nobody@fabrica.example"}`

func TestTheReadOfTheSetupAndEveryOtherPathHaveNoLimit(t *testing.T) {
	t.Parallel()
	caddy := behindCaddy(t, startAPI(t, onTheMachine(t, databasetest.Migrated(t))))
	// Past the limits of the setup and of the login code first, so that what
	// follows comes from an address that is over both.
	for range 11 {
		caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode)
		caddy.send(t, http.MethodPost, "/api/session/login_code", anEmailOfNoUser)
	}
	require.Equal(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode).status)
	require.Equal(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, "/api/session/login_code", anEmailOfNoUser).status)

	for range 15 {
		assert.NotEqual(t, http.StatusTooManyRequests, caddy.send(t, http.MethodGet, "/api/setup", "").status)
		assert.NotEqual(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, "/api/session/log_out", "{}").status)
		assert.NotEqual(t, http.StatusTooManyRequests, caddy.send(t, http.MethodGet, "/setup", "").status)
	}
}

// composeNetwork is the network of the services of compose.yaml, where
// Garage is reached by the name of its service.
const composeNetwork = "software-factory-manager_default"

// filesBehindCaddy starts Caddy with the Caddyfile of development on the
// network of the services, so that the host name of the files leads to the
// Garage of development, as it does in compose.yaml. It returns the address
// Caddy listens on.
func filesBehindCaddy(t *testing.T) string {
	t.Helper()
	container := docker(t, "run", "--detach", "--rm",
		"--label", "software-factory-manager.test=caddy",
		"--network", composeNetwork,
		"--publish", "127.0.0.1::80",
		"--mount", mountOf(t, caddyfile, "/etc/caddy/Caddyfile"),
		"--mount", mountOf(t, deliveryCaddyfile, "/etc/caddy/delivery.caddyfile"),
		"--env", "OTEL_TRACES_EXPORTER=none",
		caddyImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", container).Run()
	})
	published := strings.Fields(docker(t, "port", container, "80/tcp"))
	require.NotEmpty(t, published)
	require.Eventually(t, func() bool {
		response, err := http.Get("http://" + published[0] + "/")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return true
	}, 30*time.Second, 50*time.Millisecond, "Caddy did not answer: %s", docker(t, "logs", container))
	return published[0]
}

// throughCaddy sends a request to Caddy as a browser on the machine sends it
// to the URL: with the host name of the URL as its Host.
func throughCaddy(t *testing.T, caddy, method, signedURL string, headers map[string]string, body []byte) *http.Response {
	t.Helper()
	target, err := url.Parse(signedURL)
	require.NoError(t, err)
	host := target.Host
	target.Host = caddy
	request, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
	require.NoError(t, err)
	request.Host = host
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response
}

// The browser sends an image to the URL the API signed, at the host name of
// the files. Caddy forwards it to Garage with the Host the signature covers,
// and Garage answers the cross-origin questions from the rule of its bucket.
func TestThroughCaddyTheHostNameOfTheFilesLeadsToTheStorage(t *testing.T) {
	t.Parallel()
	temporary := databasetest.Migrated(t)
	_, code := issueSetupCodeIn(t, temporary)
	api := startAPI(t, environmentOf(temporary))
	started := startUpload(t, api, uploadBody(code, "consultancy_logo", "image/png", 2048))
	require.Equal(t, http.StatusCreated, started.status, started.body)
	signedURL, _ := started.fields["upload_url"].(string)
	caddy := filesBehindCaddy(t)
	appOrigin := os.Getenv("APP_ORIGIN")

	fromTheApp := throughCaddy(t, caddy, http.MethodOptions, signedURL, map[string]string{
		"Origin": appOrigin, "Access-Control-Request-Method": http.MethodPut, "Access-Control-Request-Headers": "content-type",
	}, nil)
	fromElsewhere := throughCaddy(t, caddy, http.MethodOptions, signedURL, map[string]string{
		"Origin": "http://elsewhere.localhost", "Access-Control-Request-Method": http.MethodPut,
	}, nil)
	sent := throughCaddy(t, caddy, http.MethodPut, signedURL, map[string]string{
		"Origin": appOrigin, "Content-Type": "image/png",
	}, aPNG(2048))

	assert.Equal(t, http.StatusOK, fromTheApp.StatusCode)
	assert.Equal(t, appOrigin, fromTheApp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusForbidden, fromElsewhere.StatusCode, "the storage lets another origin send a file")
	assert.Equal(t, http.StatusOK, sent.StatusCode, "the storage refused the image sent through Caddy")
	assert.Equal(t, appOrigin, sent.Header.Get("Access-Control-Allow-Origin"))
}

func TestLivenessAndReadinessCannotBeReachedThroughCaddy(t *testing.T) {
	t.Parallel()
	caddy := behindCaddy(t, startAPI(t, onTheMachine(t, databasetest.Migrated(t))))

	for _, path := range []string{"/livez", "/readyz", "/api/../readyz", "/api/%2e%2e/livez", "/api/..%2freadyz"} {
		answer := caddy.send(t, http.MethodGet, path, "")

		assert.Equal(t, theApp, answer.body, "%s reached something other than the app: %d", path, answer.status)
	}
}

func TestUpAnswersTheStatusOfReadinessWithNoBody(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		environment func(t *testing.T) map[string]string
		status      int
	}{
		"when the API is ready": {
			environment: func(t *testing.T) map[string]string { return onTheMachine(t, databasetest.Migrated(t)) },
			status:      http.StatusOK,
		},
		"when PostgreSQL does not answer": {
			environment: func(t *testing.T) map[string]string {
				return withFileStorage(map[string]string{
					"API_LISTEN_ADDRESS": net.JoinHostPort(machineHost(t), "0"),
					"APP_ORIGIN":         appOrigin(),
					"LOGIN_CODE_KEY":     theLoginCodeKey,
					// Port 1 of the machine has no PostgreSQL behind it.
					"DATABASE_URL": "postgres://api:api@127.0.0.1:1/software_factory?sslmode=disable",
				})
			},
			status: http.StatusServiceUnavailable,
		},
	}
	for name, ready := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			caddy := behindCaddy(t, startAPI(t, ready.environment(t)))

			up := caddy.send(t, http.MethodGet, "/up", "")

			assert.Equal(t, ready.status, up.status)
			assert.Empty(t, up.body)
		})
	}
}

// Every failure under /api is in the error format, the ones Caddy answers
// for an API it cannot reach too: the app translates them by their code and
// shows the trace Caddy started.
func TestCaddyAnswersInTheErrorFormatWhenTheAPICannotBeReached(t *testing.T) {
	t.Parallel()
	api := launchAPI(t, onTheMachine(t, databasetest.Migrated(t)))
	caddy := behindCaddy(t, api.url)
	api.signal()
	require.Equal(t, 0, <-api.status, "the API stopped with: %s", api.logs)

	requests := []struct{ method, path, body string }{
		{http.MethodGet, "/api/setup", ""},
		{http.MethodPost, "/api/setup/open", wrongSetupCode},
	}
	for _, request := range requests {
		answer := caddy.send(t, request.method, request.path, request.body)

		require.Equal(t, http.StatusBadGateway, answer.status, "%s %s: %s", request.method, request.path, answer.body)
		assert.Equal(t, "application/problem+json", answer.mediaType)
		assert.Equal(t, "no-store", answer.header.Get("Cache-Control"))
		traceID, _ := answer.fields["trace_id"].(string)
		assert.Regexp(t, `^[0-9a-f]{32}$`, traceID, "Caddy starts the trace of the request")
		assert.JSONEq(t, `{
			"title": "Bad Gateway",
			"status": 502,
			"code": "bad_gateway",
			"params": {},
			"trace_id": "`+traceID+`"
		}`, answer.body)
		assertIsAProblem(t, answer.body)
	}
}

// output is what Caddy wrote on the output of its container: its logs.
func (s site) output(t *testing.T) string {
	t.Helper()
	printed, err := exec.CommandContext(t.Context(), "docker", "logs", s.container).CombinedOutput()
	require.NoError(t, err, "docker logs: %s", printed)
	return string(printed)
}

// standIn listens on the machine where Caddy reaches it, as the API, Garage
// or Mailpit would, and answers 204 to every request, after it recorded what
// Caddy forwarded: the address of the caller as Caddy saw it, the Host
// header and the path.
type standIn struct {
	server             *httptest.Server
	caller, host, path atomic.Value
}

func startStandIn(t *testing.T) *standIn {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(machineHost(t), "0"))
	require.NoError(t, err)
	stand := &standIn{}
	for _, seen := range []*atomic.Value{&stand.caller, &stand.host, &stand.path} {
		seen.Store("")
	}
	stand.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stand.caller.Store(r.Header.Get("X-Forwarded-For"))
		stand.host.Store(r.Host)
		stand.path.Store(r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	stand.server.Listener = listener
	stand.server.Start()
	t.Cleanup(stand.server.Close)
	return stand
}

// callerSeen is the address of the caller of the last request, as Caddy
// forwarded it.
func (s *standIn) callerSeen() string {
	text, _ := s.caller.Load().(string)
	return text
}

// hostSeen is the Host header of the last request.
func (s *standIn) hostSeen() string {
	text, _ := s.host.Load().(string)
	return text
}

// pathSeen is the path of the last request.
func (s *standIn) pathSeen() string {
	text, _ := s.path.Load().(string)
	return text
}

// port is the port of the machine the stand-in listens on.
func (s *standIn) port(t *testing.T) string {
	t.Helper()
	_, port, err := net.SplitHostPort(s.server.Listener.Addr().String())
	require.NoError(t, err)
	return port
}

// The output of Caddy carries the address of a caller cut down to its
// network, and never whole (Privacy in Observability): the line the limit
// writes when it refuses a request, and the one of a request the API could
// not answer.
func TestTheOutputOfCaddyCarriesNoAddressOfACallerWhole(t *testing.T) {
	t.Parallel()
	api := startStandIn(t)
	caddy := behindCaddy(t, api.server.URL)
	for range 10 {
		require.Equal(t, http.StatusNoContent, caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode).status)
	}
	require.Equal(t, http.StatusTooManyRequests, caddy.send(t, http.MethodPost, "/api/setup/open", wrongSetupCode).status)
	caller := api.callerSeen()
	require.NotEmpty(t, caller, "Caddy forwarded no address of the caller")
	api.server.Close()
	require.Equal(t, http.StatusBadGateway, caddy.send(t, http.MethodGet, "/api/setup", "").status)

	logs := caddy.output(t)

	assert.Contains(t, logs, "rate limit exceeded", "the limit wrote no line")
	assert.NotContains(t, logs, `"`+caller+`"`, "the output of Caddy carries the whole address of the caller: %s", logs)
	assert.Contains(t, logs, `"remote_ip":"`+networkOf(t, caller)+`"`, "the line of the limit lost the network of the caller")
}

// networkOf cuts an address down to its network, as the Caddyfile does: 24
// bits of an IPv4 address, 48 of an IPv6 one.
func networkOf(t *testing.T, address string) string {
	t.Helper()
	ip := net.ParseIP(address)
	require.NotNil(t, ip, "%q is no address", address)
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String()
	}
	return ip.Mask(net.CIDRMask(48, 128)).String()
}

func TestUpAnswersAFailureWhenTheAPIIsStopped(t *testing.T) {
	t.Parallel()
	api := launchAPI(t, onTheMachine(t, databasetest.Migrated(t)))
	caddy := behindCaddy(t, api.url)
	api.signal()
	require.Equal(t, 0, <-api.status, "the API stopped with: %s", api.logs)

	up := caddy.send(t, http.MethodGet, "/up", "")

	assert.GreaterOrEqual(t, up.status, http.StatusInternalServerError)
	assert.Empty(t, up.body)
}
