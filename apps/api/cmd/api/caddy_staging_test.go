package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Caddy as staging runs it (Delivery in CODING_STANDARDS.md): the Caddyfile
// of staging and the rules of delivery it imports, mounted as
// deploy/compose.staging.yaml mounts them, over HTTPS, in front of stand-ins
// for the API, Garage and Mailpit that listen on the machine. The host names
// are under localhost, so Caddy gives them certificates of its own local
// authority, which the client of the test trusts, where the server gets them
// from Let's Encrypt. The built app is a fixture mounted where the Caddy
// image of the servers holds the app, /srv, so the test runs on the Caddy
// image of development, which `task services` builds.

const (
	// stagingCaddyfile is the Caddyfile of staging.
	stagingCaddyfile = "../../../../deploy/Caddyfile.staging"
	// The three host names, as the publish derives them from the address of
	// the server.
	stagingApp   = "https://app.staging.localhost"
	stagingFiles = "https://files.staging.localhost"
	stagingMail  = "https://mail.staging.localhost"
	// The password of Mailpit and its bcrypt hash, which Caddy holds, as
	// `caddy hash-password` writes it.
	mailpitPassword     = "password-of-the-test"
	mailpitPasswordHash = "$2a$14$us/GKk8MgW664ehzPoqAkuTdT8MtuD8aNb5vmcnGhyXq03bJzWvTe"
	// The files of the fixture of the built app: index.html, and a script
	// with a hash in its name, long enough to be compressed, beside a copy
	// that stands for its Brotli copy.
	theIndex         = "<!doctype html><title>the built app</title>"
	hashedScript     = "/assets/index-D4Nk3x7Q.js"
	theBrotliCopy    = "the Brotli copy of the script"
	localRootOfCaddy = "/data/caddy/pki/authorities/local/root.crt"
)

// theScript is the content of the script of the fixture.
var theScript = strings.Repeat("console.log('the built app')\n", 40)

// stagingSite is Caddy serving the three host names of staging.
type stagingSite struct {
	client    *http.Client
	container string
}

// theBuiltApp writes the fixture of the built app into a folder of its own.
func theBuiltApp(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	files := map[string]string{
		"index.html":         theIndex,
		hashedScript:         theScript,
		hashedScript + ".br": theBrotliCopy,
	}
	for name, content := range files {
		file := filepath.Join(folder, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
		require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	}
	return folder
}

// upstreamsOfStaging are the stand-ins Caddy forwards to.
type upstreamsOfStaging struct {
	api, files, mail *standIn
}

func standInsForStaging(t *testing.T) upstreamsOfStaging {
	t.Helper()
	return upstreamsOfStaging{api: startStandIn(t), files: startStandIn(t), mail: startStandIn(t)}
}

// onStaging starts Caddy with the Caddyfile of staging in front of the
// stand-ins, and waits until the app answers over HTTPS.
func onStaging(t *testing.T, upstreams upstreamsOfStaging) stagingSite {
	t.Helper()
	requireCaddyImage(t)
	container := docker(t, "run", "--detach", "--rm",
		"--label", "software-factory-manager.test=caddy",
		"--publish", "127.0.0.1::80",
		"--publish", "127.0.0.1::443",
		"--add-host", "host.docker.internal:host-gateway",
		"--mount", mountOf(t, stagingCaddyfile, "/etc/caddy/Caddyfile"),
		"--mount", mountOf(t, deliveryCaddyfile, "/etc/caddy/delivery.caddyfile"),
		"--mount", mountOf(t, theBuiltApp(t), "/srv"),
		"--env", "OTEL_TRACES_EXPORTER=none",
		"--env", "APP_ORIGIN="+stagingApp,
		"--env", "FILE_STORAGE_PUBLIC_URL="+stagingFiles,
		"--env", "MAILPIT_PUBLIC_URL="+stagingMail,
		"--env", "MAILPIT_PASSWORD_HASH="+mailpitPasswordHash,
		"--env", "ACME_EMAIL=",
		"--env", "API_UPSTREAM=host.docker.internal:"+upstreams.api.port(t),
		"--env", "FILES_UPSTREAM=host.docker.internal:"+upstreams.files.port(t),
		"--env", "MAIL_UPSTREAM=host.docker.internal:"+upstreams.mail.port(t),
		caddyImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", container).Run()
	})
	staging := stagingSite{container: container, client: clientOfStaging(t, container)}
	answered := assert.Eventually(t, func() bool {
		response, err := staging.client.Get(stagingApp + "/")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return true
	}, 30*time.Second, 50*time.Millisecond)
	if !answered {
		require.FailNow(t, "Caddy did not answer over HTTPS", site{container: container}.output(t))
	}
	return staging
}

// clientOfStaging is a browser on the machine: it sends every host name to
// the ports Docker published, and trusts the local authority of Caddy alone.
func clientOfStaging(t *testing.T, container string) *http.Client {
	t.Helper()
	published := map[string]string{
		"80":  publishedPort(t, container, "80/tcp"),
		"443": publishedPort(t, container, "443/tcp"),
	}
	authorities := x509.NewCertPool()
	created := assert.Eventually(t, func() bool {
		root, err := exec.CommandContext(t.Context(), "docker", "exec", container, "cat", localRootOfCaddy).Output()
		return err == nil && authorities.AppendCertsFromPEM(root)
	}, 30*time.Second, 50*time.Millisecond)
	if !created {
		require.FailNow(t, "Caddy created no local authority", site{container: container}.output(t))
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, published[port])
			},
			TLSClientConfig: &tls.Config{RootCAs: authorities, MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// publishedPort is the address of the machine where Docker published a port
// of the container.
func publishedPort(t *testing.T, container, port string) string {
	t.Helper()
	published := strings.Fields(docker(t, "port", container, port))
	require.NotEmpty(t, published)
	return published[0]
}

// stagingAnswer is what a host name of staging answered.
type stagingAnswer struct {
	status int
	header http.Header
	body   string
}

// send sends a request to a URL of staging, with the headers given.
func (s stagingSite) send(t *testing.T, method, address string, headers map[string]string, body string) stagingAnswer {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, address, strings.NewReader(body))
	require.NoError(t, err)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := s.client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return stagingAnswer{status: response.StatusCode, header: response.Header, body: string(raw)}
}

func (s stagingSite) get(t *testing.T, address string) stagingAnswer {
	t.Helper()
	return s.send(t, http.MethodGet, address, nil, "")
}

func TestStagingServesTheBuiltAppOverHTTPS(t *testing.T) {
	t.Parallel()
	caddy := onStaging(t, standInsForStaging(t))

	plain := caddy.get(t, "http://app.staging.localhost/setup")
	index := caddy.get(t, stagingApp+"/")
	route := caddy.get(t, stagingApp+"/setup")
	hashed := caddy.get(t, stagingApp+hashedScript)
	missing := caddy.get(t, stagingApp+"/assets/index-M1ss1ng0.js")

	assert.Equal(t, http.StatusPermanentRedirect, plain.status)
	assert.Equal(t, stagingApp+"/setup", plain.header.Get("Location"))
	for name, page := range map[string]stagingAnswer{"/": index, "a route of the app": route} {
		assert.Equal(t, http.StatusOK, page.status, name)
		assert.Equal(t, theIndex, page.body, name)
		assert.Equal(t, "no-cache", page.header.Get("Cache-Control"), name)
	}
	assert.Equal(t, http.StatusOK, hashed.status)
	assert.Equal(t, theScript, hashed.body)
	assert.Equal(t, "public, max-age=31536000, immutable", hashed.header.Get("Cache-Control"))
	assert.Equal(t, http.StatusNotFound, missing.status, "a file of /assets/ that does not exist is not the app")
	assert.Empty(t, missing.header.Get("Cache-Control"), "a failure carries a cache header, which the browser would keep")
}

func TestStagingSendsTheBrotliCopyOrCompressesTheFile(t *testing.T) {
	t.Parallel()
	caddy := onStaging(t, standInsForStaging(t))

	brotli := caddy.send(t, http.MethodGet, stagingApp+hashedScript, map[string]string{"Accept-Encoding": "br, gzip"}, "")
	gzipped := caddy.send(t, http.MethodGet, stagingApp+hashedScript, map[string]string{"Accept-Encoding": "gzip"}, "")

	assert.Equal(t, "br", brotli.header.Get("Content-Encoding"))
	assert.Equal(t, theBrotliCopy, brotli.body)
	require.Equal(t, "gzip", gzipped.header.Get("Content-Encoding"))
	reader, err := gzip.NewReader(bytes.NewReader([]byte(gzipped.body)))
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, theScript, string(decompressed))
}

// directivesOf reads a Content-Security-Policy into its directives, each with
// its sources.
func directivesOf(policy string) map[string]string {
	directives := map[string]string{}
	for _, directive := range strings.Split(policy, ";") {
		name, sources, _ := strings.Cut(strings.TrimSpace(directive), " ")
		directives[name] = sources
	}
	return directives
}

func TestStagingSetsTheSecurityHeadersOnTheSiteOfTheApp(t *testing.T) {
	t.Parallel()
	caddy := onStaging(t, standInsForStaging(t))

	page := caddy.get(t, stagingApp+"/")

	assert.Equal(t, map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self'",
		"style-src":       "'self'",
		"style-src-elem":  "'self' 'unsafe-inline'",
		"font-src":        "'self'",
		"img-src":         "'self' blob: " + stagingFiles,
		"media-src":       "'self' blob: " + stagingFiles,
		"connect-src":     "'self' " + stagingFiles,
		"object-src":      "'none'",
		"base-uri":        "'none'",
		"frame-ancestors": "'none'",
	}, directivesOf(page.header.Get("Content-Security-Policy")))
	assert.Equal(t, "max-age=31536000", page.header.Get("Strict-Transport-Security"))
	assert.Equal(t, "nosniff", page.header.Get("X-Content-Type-Options"))
	assert.Equal(t, "same-origin", page.header.Get("Referrer-Policy"))
}

func TestStagingForwardsTheAPIAndAnswersUpWithItsReadiness(t *testing.T) {
	t.Parallel()
	upstreams := standInsForStaging(t)
	caddy := onStaging(t, upstreams)

	api := caddy.get(t, stagingApp+"/api/setup")
	assert.Equal(t, http.StatusNoContent, api.status)
	assert.Equal(t, "no-store", api.header.Get("Cache-Control"))
	assert.Equal(t, "/api/setup", upstreams.api.pathSeen())
	assert.Equal(t, "app.staging.localhost", upstreams.api.hostSeen())

	up := caddy.get(t, stagingApp+"/up")
	assert.Equal(t, http.StatusNoContent, up.status, "/up keeps the status of readiness")
	assert.Empty(t, up.body)
	assert.Equal(t, "/readyz", upstreams.api.pathSeen())
}

// The browser sends the bytes of a file to the URL the API signed for the
// host name of the files: the Host header reaches Garage unchanged, because
// the signature covers it.
func TestStagingTheHostNameOfTheFilesLeadsToTheStorage(t *testing.T) {
	t.Parallel()
	upstreams := standInsForStaging(t)
	caddy := onStaging(t, upstreams)

	sent := caddy.send(t, http.MethodPut, stagingFiles+"/software-factory-files/uploads/a?X-Amz-Signature=0", map[string]string{"Content-Type": "image/png"}, "an image")

	assert.Equal(t, http.StatusNoContent, sent.status)
	assert.Equal(t, "files.staging.localhost", upstreams.files.hostSeen())
	assert.Equal(t, "/software-factory-files/uploads/a", upstreams.files.pathSeen())
	assert.Empty(t, upstreams.api.pathSeen(), "the files reached the API")
}

func TestStagingMailpitAsksForThePassword(t *testing.T) {
	t.Parallel()
	upstreams := standInsForStaging(t)
	caddy := onStaging(t, upstreams)
	withPassword := func(password string) map[string]string {
		request, err := http.NewRequest(http.MethodGet, stagingMail, nil)
		require.NoError(t, err)
		request.SetBasicAuth("maintainer", password)
		return map[string]string{"Authorization": request.Header.Get("Authorization")}
	}

	unknown := caddy.get(t, stagingMail+"/")
	wrong := caddy.send(t, http.MethodGet, stagingMail+"/", withPassword("not the password"), "")
	require.Empty(t, upstreams.mail.pathSeen(), "Mailpit was reached with no password")
	known := caddy.send(t, http.MethodGet, stagingMail+"/api/v1/messages", withPassword(mailpitPassword), "")

	assert.Equal(t, http.StatusUnauthorized, unknown.status)
	assert.Contains(t, unknown.header.Get("WWW-Authenticate"), "Basic")
	assert.Equal(t, http.StatusUnauthorized, wrong.status)
	assert.Equal(t, http.StatusNoContent, known.status)
	assert.Equal(t, "/api/v1/messages", upstreams.mail.pathSeen())
}

// pastTheLimit sends ten POST a minute to a path, which the stand-in for the
// API answers, and returns what the eleventh got.
func (s stagingSite) pastTheLimit(t *testing.T, path string) stagingAnswer {
	t.Helper()
	for range 10 {
		require.Equal(t, http.StatusNoContent, s.send(t, http.MethodPost, stagingApp+path, nil, "{}").status, "POST %s", path)
	}
	return s.send(t, http.MethodPost, stagingApp+path, nil, "{}")
}

// The limits are the ones of development, from the same rules, and so is the
// output of Caddy: the address of a caller cut down to its network.
func TestStagingLimitsTheSetupCodeAndTheLoginCode(t *testing.T) {
	t.Parallel()
	upstreams := standInsForStaging(t)
	caddy := onStaging(t, upstreams)

	for _, path := range []string{"/api/setup/open", "/api/session/login_code"} {
		over := caddy.pastTheLimit(t, path)

		require.Equal(t, http.StatusTooManyRequests, over.status, "POST %s: %s", path, over.body)
		assert.Equal(t, "application/problem+json", over.header.Get("Content-Type"))
		assert.Equal(t, "no-store", over.header.Get("Cache-Control"))
		assert.NotEmpty(t, over.header.Get("Retry-After"))
		assert.Contains(t, over.body, `"code":"too_many_requests"`)
		assertIsAProblem(t, over.body)
	}
	caller := upstreams.api.callerSeen()
	require.NotEmpty(t, caller, "Caddy forwarded no address of the caller")

	logs := site{container: caddy.container}.output(t)

	assert.Contains(t, logs, "rate limit exceeded", "the limit wrote no line")
	assert.NotContains(t, logs, `"`+caller+`"`, "the output of Caddy carries the whole address of the caller")
	assert.Contains(t, logs, `"remote_ip":"`+networkOf(t, caller)+`"`, "the line of the limit lost the network of the caller")
}
