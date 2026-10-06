// Command whoami is a small HTTP server that reports what Tailscale PAM told
// it about the caller, and — more usefully — how much of that it can prove.
//
// A PAM HTTP service forwards the authenticated user's identity upstream in
// X-Auth-* headers, and signs part of that set with an Ed25519 key published
// as a JSON Web Key Set (JWKS). This server reconstructs the canonical
// request, checks the signature, and renders the headers in two groups: the
// ones covered by the signature and the ones that are not.
//
// It deliberately does not reject unsigned requests. Being able to see a
// forged header land in the "not covered" column is the point.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Headers covered by the signature, as implemented by the reference verifier
// at github.com/borderzero/examples/httpsig. Only these can be trusted once
// the signature checks out.
var signedHeaders = []string{
	"Host",
	"X-Auth-Timestamp",
	"X-Auth-Request-Id",
	"X-Auth-Kid",
	"X-Auth-Email",
	"X-Auth-Name",
	"X-Auth-Picture",
	"X-Auth-Subject",
	"X-Auth-IsServiceAccount",
}

// Identity headers PAM sends that the signature does not cover. Useful for
// display, not for authorization.
var unsignedHeaders = []string{
	"X-Auth-Username",
	"X-Auth-Userid",
	"X-Auth-Expiresin",
}

const (
	statusVerified   = "verified"
	statusInvalid    = "invalid"
	statusUnsigned   = "unsigned"
	statusUnknownKey = "unknown_key"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	jwksURL := flag.String("jwks-url", "https://signing.border0.io/keys", "JWKS endpoint holding the PAM signing keys")
	flag.Parse()

	ks := &keySet{url: *jwksURL}
	if err := ks.refresh(); err != nil {
		// Not fatal: the server is still useful for showing unsigned
		// requests, and the key set is retried on the first request
		// carrying a key ID we do not recognise.
		log.Printf("could not load signing keys from %s: %v", ks.url, err)
	} else {
		log.Printf("loaded %d signing key(s) from %s", ks.count(), ks.url)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", handler(ks))

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on %s", *listen)
	log.Fatal(srv.ListenAndServe())
}

func handler(ks *keySet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rep := buildReport(r, ks)
		log.Printf("%s %s signature=%s email=%q", r.Method, r.URL.Path, rep.Signature.Status, r.Header.Get("X-Auth-Email"))

		if wantsJSON(r) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rep); err != nil {
				log.Printf("writing JSON response: %v", err)
			}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(w, rep); err != nil {
			log.Printf("rendering page: %v", err)
		}
	})
}

// wantsJSON picks a representation. An explicit ?format= wins, so that a
// browser can be pointed at the JSON too; otherwise anything that does not
// ask for HTML (curl, for one) gets JSON.
func wantsJSON(r *http.Request) bool {
	switch r.URL.Query().Get("format") {
	case "json":
		return true
	case "html":
		return false
	}
	return !strings.Contains(r.Header.Get("Accept"), "text/html")
}

type field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type sigInfo struct {
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	KeyID      string `json:"key_id,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	AgeSeconds *int64 `json:"age_seconds,omitempty"`
	Canonical  string `json:"canonical_request"`
}

type report struct {
	Signature sigInfo `json:"signature"`
	Covered   []field `json:"in_signed_header_set"`
	Uncovered []field `json:"outside_signed_header_set"`
	Other     []field `json:"other_auth_headers,omitempty"`

	// Trusted is true only when the signature verified, and governs how the
	// HTML page labels the covered headers.
	Trusted bool `json:"-"`
}

func buildReport(r *http.Request, ks *keySet) report {
	canonical := canonicalRequest(r)

	rep := report{
		Signature: sigInfo{
			KeyID:     r.Header.Get("X-Auth-Kid"),
			RequestID: r.Header.Get("X-Auth-Request-Id"),
			Timestamp: r.Header.Get("X-Auth-Timestamp"),
			Canonical: canonical,
		},
	}
	if t, ok := parseTimestamp(rep.Signature.Timestamp); ok {
		age := int64(time.Since(t).Seconds())
		rep.Signature.AgeSeconds = &age
	}

	switch sig := r.Header.Get("X-Auth-Sig"); {
	case sig == "":
		rep.Signature.Status = statusUnsigned
		rep.Signature.Detail = "There is no X-Auth-Sig header, so this request did not arrive through a Tailscale PAM HTTP service. Every X-Auth-* header below was set by whoever made the request."

	case rep.Signature.KeyID == "":
		rep.Signature.Status = statusInvalid
		rep.Signature.Detail = "X-Auth-Sig is present but X-Auth-Kid is not, so there is no key to check it against."

	default:
		raw, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			rep.Signature.Status = statusInvalid
			rep.Signature.Detail = "X-Auth-Sig is not valid base64: " + err.Error()
			break
		}
		key, ok := ks.lookup(rep.Signature.KeyID)
		if !ok {
			rep.Signature.Status = statusUnknownKey
			rep.Signature.Detail = fmt.Sprintf("Key %q was not published at %s. The signing key may have rotated, or the signature may not be from Tailscale PAM at all.", rep.Signature.KeyID, ks.url)
			break
		}
		if !ed25519.Verify(key, []byte(canonical), raw) {
			rep.Signature.Status = statusInvalid
			rep.Signature.Detail = "The signature did not match the canonical request shown below. Either a signed header was rewritten in transit, or the request was not signed by the key it names."
			break
		}
		rep.Signature.Status = statusVerified
		rep.Signature.Detail = "The signature matches the canonical request, so Tailscale PAM vouches for every header in the first group."
	}
	rep.Trusted = rep.Signature.Status == statusVerified

	for _, name := range signedHeaders {
		if v := headerValues(r, name); len(v) > 0 {
			rep.Covered = append(rep.Covered, field{name, strings.Join(v, ",")})
		}
	}
	for _, name := range unsignedHeaders {
		if v := headerValues(r, name); len(v) > 0 {
			rep.Uncovered = append(rep.Uncovered, field{name, strings.Join(v, ",")})
		}
	}
	rep.Other = otherAuthHeaders(r)

	return rep
}

// canonicalRequest rebuilds the string the signer covered:
//
//	METHOD\nPATH\nQUERY\nHEADERS
//
// where HEADERS is one "lowercase-name:value" line per signed header that is
// actually present, multi-valued headers joined with commas, sorted.
// Binding the method, path and query in means a signature cannot be lifted
// off one request and replayed on another.
func canonicalRequest(r *http.Request) string {
	pairs := make([]string, 0, len(signedHeaders))
	for _, name := range signedHeaders {
		if v := headerValues(r, name); len(v) > 0 {
			pairs = append(pairs, strings.ToLower(name)+":"+strings.Join(v, ","))
		}
	}
	sort.Strings(pairs)

	return strings.Join([]string{
		r.Method,
		r.URL.Path,
		r.URL.RawQuery,
		strings.Join(pairs, "\n"),
	}, "\n")
}

// headerValues reads a header, accounting for Host living outside the header
// map in Go's server.
func headerValues(r *http.Request, name string) []string {
	if strings.EqualFold(name, "Host") {
		if r.Host == "" {
			return nil
		}
		return []string{r.Host}
	}
	return r.Header.Values(name)
}

// otherAuthHeaders returns any X-Auth-* header this server does not already
// know about, so that headers added to PAM after this was written still show
// up somewhere rather than being silently dropped.
func otherAuthHeaders(r *http.Request) []field {
	known := make(map[string]bool, len(signedHeaders)+len(unsignedHeaders)+1)
	for _, n := range append(append([]string{"X-Auth-Sig"}, signedHeaders...), unsignedHeaders...) {
		known[http.CanonicalHeaderKey(n)] = true
	}

	var out []field
	for name, values := range r.Header {
		if known[name] || !strings.HasPrefix(http.CanonicalHeaderKey(name), "X-Auth-") {
			continue
		}
		out = append(out, field{name, strings.Join(values, ",")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// parseTimestamp accepts the Unix epoch at any of the usual resolutions, or
// RFC 3339, since the wire format is not contractual.
func parseTimestamp(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case n > 1e17:
			return time.Unix(0, n), true
		case n > 1e14:
			return time.UnixMicro(n), true
		case n > 1e11:
			return time.UnixMilli(n), true
		default:
			return time.Unix(n, 0), true
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// keySet holds the Ed25519 public keys from the JWKS, refreshed lazily when a
// request names a key ID we have not seen.
type keySet struct {
	url string

	mu        sync.RWMutex
	keys      map[string]ed25519.PublicKey
	lastFetch time.Time
}

const refreshCooldown = time.Minute

func (ks *keySet) lookup(kid string) (ed25519.PublicKey, bool) {
	ks.mu.RLock()
	key, ok := ks.keys[kid]
	cold := time.Since(ks.lastFetch) > refreshCooldown
	ks.mu.RUnlock()

	if ok || !cold {
		return key, ok
	}

	// An unfamiliar key ID usually means the signing key rotated after we
	// started up. The cooldown keeps an unknown key from turning into a
	// fetch per request.
	if err := ks.refresh(); err != nil {
		log.Printf("refreshing signing keys: %v", err)
	}

	ks.mu.RLock()
	defer ks.mu.RUnlock()
	key, ok = ks.keys[kid]
	return key, ok
}

func (ks *keySet) refresh() error {
	ks.mu.Lock()
	ks.lastFetch = time.Now() // recorded up front, so a failure also backs off
	ks.mu.Unlock()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ks.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", ks.url, resp.Status)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("parsing JWKS: %w", err)
	}

	keys := make(map[string]ed25519.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" {
			continue
		}
		// JWKS encodes the key with base64url and no padding, whereas
		// X-Auth-Sig arrives as standard base64. They are not interchangeable.
		b, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			log.Printf("skipping key %s: %v", k.Kid, err)
			continue
		}
		if len(b) != ed25519.PublicKeySize {
			log.Printf("skipping key %s: got %d bytes, want %d", k.Kid, len(b), ed25519.PublicKeySize)
			continue
		}
		keys[k.Kid] = ed25519.PublicKey(b)
	}
	if len(keys) == 0 {
		return fmt.Errorf("no Ed25519 keys found at %s", ks.url)
	}

	ks.mu.Lock()
	ks.keys = keys
	ks.mu.Unlock()
	return nil
}

func (ks *keySet) count() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.keys)
}

var page = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PAM whoami</title>
<style>
  :root { color-scheme: light dark; }
  body {
    font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
    max-width: 48rem; margin: 2.5rem auto; padding: 0 1.25rem;
  }
  h1 { font-size: 1.4rem; margin: 0 0 1.5rem; }
  h2 { font-size: 1rem; margin: 2rem 0 .5rem; }
  p.note { color: #6b7280; margin: .25rem 0 .75rem; }
  .banner { border-radius: .5rem; padding: .9rem 1.1rem; border: 1px solid; }
  .banner .status { font-weight: 600; letter-spacing: .02em; text-transform: uppercase; font-size: .8rem; }
  .banner p { margin: .4rem 0 0; }
  .verified    { background: #ecfdf5; border-color: #6ee7b7; color: #065f46; }
  .unsigned    { background: #fffbeb; border-color: #fcd34d; color: #92400e; }
  .invalid,
  .unknown_key { background: #fef2f2; border-color: #fca5a5; color: #991b1b; }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: .4rem .6rem; border-bottom: 1px solid #e5e7eb; vertical-align: top; }
  th { width: 15rem; font-weight: 600; }
  td { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .85rem; word-break: break-all; }
  pre {
    background: #f9fafb; border: 1px solid #e5e7eb; border-radius: .4rem;
    padding: .8rem; overflow-x: auto; font-size: .8rem; margin: 0;
  }
  footer { margin-top: 2.5rem; color: #6b7280; font-size: .85rem; }
  @media (prefers-color-scheme: dark) {
    .verified    { background: #052e23; border-color: #047857; color: #6ee7b7; }
    .unsigned    { background: #2e2206; border-color: #b45309; color: #fcd34d; }
    .invalid,
    .unknown_key { background: #330d0d; border-color: #b91c1c; color: #fca5a5; }
    th, td { border-bottom-color: #374151; }
    pre { background: #111827; border-color: #374151; }
  }
</style>
</head>
<body>
<h1>Tailscale PAM &mdash; whoami</h1>

<div class="banner {{.Signature.Status}}">
  <div class="status">Signature: {{.Signature.Status}}</div>
  <p>{{.Signature.Detail}}</p>
</div>

{{if .Covered}}
<h2>{{if .Trusted}}Verified identity{{else}}Claimed identity &mdash; NOT verified{{end}}</h2>
<p class="note">
  {{if .Trusted}}
    Covered by the signature. Safe to make authorization decisions on.
  {{else}}
    These headers are the ones a signature would cover, but this request has no
    valid signature, so nothing here is proven.
  {{end}}
</p>
<table>
  {{range .Covered}}<tr><th>{{.Name}}</th><td>{{.Value}}</td></tr>{{end}}
</table>
{{end}}

{{if .Uncovered}}
<h2>Not covered by the signature</h2>
<p class="note">
  Tailscale PAM sends these, but they sit outside the signed set even on a
  verified request. Display them; do not authorize on them.
</p>
<table>
  {{range .Uncovered}}<tr><th>{{.Name}}</th><td>{{.Value}}</td></tr>{{end}}
</table>
{{end}}

{{if .Other}}
<h2>Other X-Auth-* headers</h2>
<p class="note">Headers this server does not recognise. Treat as unverified.</p>
<table>
  {{range .Other}}<tr><th>{{.Name}}</th><td>{{.Value}}</td></tr>{{end}}
</table>
{{end}}

<h2>Canonical request</h2>
<p class="note">
  Rebuilt from this request as <code>METHOD</code>, path, query, then the signed
  headers &mdash; lowercased, comma-joined, sorted. This exact string is what the
  Ed25519 signature is checked against.
</p>
<pre>{{.Signature.Canonical}}</pre>

<footer>
  {{if .Signature.RequestID}}Request {{.Signature.RequestID}}{{end}}
  {{if .Signature.AgeSeconds}} &middot; signed {{.Signature.AgeSeconds}}s ago{{end}}
  <br>Add <code>?format=json</code> for the machine-readable version.
</footer>
</body>
</html>
`))
