//go:build e2e

package proxy

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
)

// want401 is the single 401 body for every authentication failure.
const want401 = `{"error":{"type":"invalid_request_error","code":"invalid_api_key","param":null,"message":"Invalid or missing API key."}}`

// wrongKey is a well-formed synthetic key that is not configured.
const wrongKey = "e2e-proxy-key-bad-Q1w2E3r4T5y6U7i8O9p0AsDf"

func chatBody() []byte {
	return []byte(`{"model":"` + localModel + `","messages":[{"role":"user","content":"привет"}]}`)
}

func authHeader(v string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if v != "" {
		h.Set("Authorization", v)
	}
	return h
}

// checkUnauthorized asserts the 401 contract, that the upstream was not
// called and that the last audit record is an unauthorized one with reason
// and without request data.
func checkUnauthorized(t *testing.T, h *harness, r response, before int, reason string) ports.AuditRecord {
	t.Helper()
	if r.status != 401 || strings.TrimSpace(string(r.body)) != want401 {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	if r.header.Get("WWW-Authenticate") != `Bearer realm="llm-proxy"` || r.header.Get("X-Request-ID") == "" {
		t.Fatalf("headers %v", r.header)
	}
	for k := range r.header {
		if strings.HasPrefix(k, "X-Ratelimit") || strings.HasPrefix(k, "X-Guard") {
			t.Fatalf("401 must not carry %s", k)
		}
	}
	if n := st.rec.Count() - before; n != 0 {
		t.Fatalf("upstream called %d times on 401", n)
	}
	recs, _ := h.auditRecords(t)
	if len(recs) == 0 {
		t.Fatal("no audit record for 401")
	}
	last := recs[len(recs)-1]
	if last.V != ports.AuditVersion || last.Status != ports.AuditStatusUnauthorized || last.HTTPStatus != 401 || last.Reason != reason ||
		last.ErrorCode != "invalid_api_key" || last.KeyID != "" || len(last.PromptMasked) != 0 || last.ClientIPHash == "" ||
		last.RequestID != r.header.Get("X-Request-ID") {
		t.Fatalf("audit record %+v", last)
	}
	return last
}

func lastKeyID(t *testing.T, h *harness) string {
	t.Helper()
	recs, _ := h.auditRecords(t)
	if len(recs) == 0 {
		t.Fatal("no audit records")
	}
	return recs[len(recs)-1].KeyID
}

func TestAuth01MissingHeader(t *testing.T) {
	extra(t, "AUTH-01", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-01", nil)
		before := st.rec.Count()
		r := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader(""))
		checkUnauthorized(t, h, r, before, ports.AuthReasonMissing)
		return "без Authorization → 401 invalid_api_key + WWW-Authenticate; upstream 0; аудит unauthorized/missing"
	})
}

func TestAuth02WrongKey(t *testing.T) {
	extra(t, "AUTH-02", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-02", nil)
		before := st.rec.Count()
		r := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader("Bearer "+wrongKey))
		checkUnauthorized(t, h, r, before, ports.AuthReasonInvalid)
		return "неверный ключ → 401 (тело как у AUTH-01); upstream 0; аудит unauthorized/invalid"
	})
}

func TestAuth03BasicScheme(t *testing.T) {
	extra(t, "AUTH-03", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-03", nil)
		before := st.rec.Count()
		r := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader("Basic "+e2eKey1))
		checkUnauthorized(t, h, r, before, ports.AuthReasonScheme)
		return "Authorization: Basic <валидный ключ> → 401; upstream 0; аудит unauthorized/scheme"
	})
}

func TestAuth04TokenInQuery(t *testing.T) {
	extra(t, "AUTH-04", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-04", nil)
		var got []string
		for _, p := range []string{"api_key", "access_token", "key", "token"} {
			before := st.rec.Count()
			r := h.raw(t, http.MethodPost, "/v1/chat/completions?"+p+"="+e2eKey1, chatBody(), authHeader("Bearer "+e2eKey1))
			checkUnauthorized(t, h, r, before, ports.AuthReasonQuery)
			got = append(got, p)
		}
		_, raw := h.auditRecords(t)
		logs, _ := os.ReadFile(filepath.Join(filepath.Dir(h.auditPath), "proxy.log"))
		if strings.Contains(raw, e2eKey1) || strings.Contains(string(logs), e2eKey1) {
			t.Fatal("query token leaked into audit or slog")
		}
		return "?" + strings.Join(got, "=…, ?") + "=… + валидный Bearer → 401 query; токен не попал в аудит и slog"
	})
}

func TestAuth05SecondKey(t *testing.T) {
	extra(t, "AUTH-05", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-05", nil)
		r1 := h.chat(t, localModel, userMsg("привет"), nil)
		id1 := lastKeyID(t, h)
		r2 := h.chat(t, localModel, userMsg("привет"), map[string]string{"Authorization": "Bearer " + e2eKey2})
		id2 := lastKeyID(t, h)
		if r1.status != 200 || r2.status != 200 {
			t.Fatalf("statuses %d %d", r1.status, r2.status)
		}
		if id1 == "" || id2 == "" || id1 == id2 || len(id1) != 12 || strings.Contains(e2eKey1+e2eKey2, id1) {
			t.Fatalf("key ids %q %q", id1, id2)
		}
		return fmt.Sprintf("второй ключ → 200; key_id %s ≠ %s (HMAC, 12 hex)", id2, id1)
	})
}

func TestAuth06LowercaseBearer(t *testing.T) {
	extra(t, "AUTH-06", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-06", nil)
		h.chat(t, localModel, userMsg("привет"), nil)
		id1 := lastKeyID(t, h)
		r := h.chat(t, localModel, userMsg("привет"), map[string]string{"Authorization": "bearer " + e2eKey1})
		if r.status != 200 || lastKeyID(t, h) != id1 {
			t.Fatalf("status %d key_id %q want %q", r.status, lastKeyID(t, h), id1)
		}
		return "схема `bearer` в нижнем регистре → 200, тот же key_id"
	})
}

func TestAuth07PublicEndpoints(t *testing.T) {
	extra(t, "AUTH-07", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-07", nil)
		before := st.rec.Count()
		r := h.raw(t, http.MethodGet, "/v1/models", nil, authHeader(""))
		checkUnauthorized(t, h, r, before, ports.AuthReasonMissing)
		hz := h.raw(t, http.MethodGet, "/healthz", nil, authHeader(""))
		rz := h.raw(t, http.MethodGet, "/readyz", nil, authHeader(""))
		if hz.status != 200 || strings.TrimSpace(string(hz.body)) != `{"status":"ok"}` || rz.status != 200 || strings.TrimSpace(string(rz.body)) != `{"status":"ready"}` {
			t.Fatalf("healthz %d %s readyz %d %s", hz.status, hz.body, rz.status, rz.body)
		}
		m := h.raw(t, http.MethodGet, "/v1/models", nil, authHeader("Bearer "+e2eKey1))
		if m.status != 200 || !strings.Contains(string(m.body), localModel) {
			t.Fatalf("models with key: %d", m.status)
		}
		nr := h.raw(t, http.MethodGet, "/admin", nil, authHeader(""))
		if nr.status != 401 {
			t.Fatalf("unknown route without key: %d", nr.status)
		}
		return "/v1/models без токена → 401, с токеном → 200; /healthz и /readyz без токена → 200 (только status); неизвестный путь без токена → 401"
	})
}

func TestAuth08InvalidJSONUnauthorized(t *testing.T) {
	extra(t, "AUTH-08", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-08", nil)
		before := st.rec.Count()
		r := h.raw(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":`), authHeader(""))
		checkUnauthorized(t, h, r, before, ports.AuthReasonMissing)
		ok := h.raw(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":`), authHeader("Bearer "+e2eKey1))
		if ok.status != 400 || ok.code != "invalid_json" {
			t.Fatalf("with key: %d %s", ok.status, ok.code)
		}
		return "невалидный JSON без ключа → 401 (тело не разбирается); с ключом → 400 invalid_json"
	})
}

func TestAuth09BruteForce(t *testing.T) {
	extra(t, "AUTH-09", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-09", nil)
		before := st.rec.Count()
		// The limit comes from auth.bruteforce in the config (no constants).
		maxFail, window := h.cfg.Auth.Bruteforce.MaxFailures, h.cfg.Auth.Bruteforce.Window
		for i := 1; i <= maxFail; i++ {
			r := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader(fmt.Sprintf("Bearer %s%02d", wrongKey[:len(wrongKey)-2], i)))
			if r.status != 401 {
				t.Fatalf("failure %d: %d", i, r.status)
			}
		}
		r11 := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader("Bearer "+wrongKey))
		ra, _ := strconv.Atoi(r11.header.Get("Retry-After"))
		if r11.status != 429 || r11.code != "rate_limit_exceeded" || ra <= 0 || ra > int(window.Seconds()) {
			t.Fatalf("over the limit: %d %s Retry-After %q", r11.status, r11.code, r11.header.Get("Retry-After"))
		}
		r12 := h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader("Bearer "+e2eKey1))
		if r12.status != 429 {
			t.Fatalf("valid key while locked: %d (key must not be checked)", r12.status)
		}
		if hz := h.raw(t, http.MethodGet, "/healthz", nil, authHeader("")); hz.status != 200 {
			t.Fatalf("healthz while locked: %d", hz.status)
		}
		if n := st.rec.Count() - before; n != 0 {
			t.Fatalf("upstream called %d times", n)
		}
		recs, _ := h.auditRecords(t)
		unauth, limited := 0, 0
		for _, rec := range recs {
			switch {
			case rec.Status == ports.AuditStatusUnauthorized && rec.Reason == ports.AuthReasonInvalid:
				unauth++
			case rec.Status == ports.AuditStatusRateLimited && rec.Reason == ports.AuthReasonFailLimit && rec.HTTPStatus == 429:
				limited++
			}
		}
		if unauth != maxFail || limited != 2 {
			t.Fatalf("audit unauthorized=%d rate_limited/auth_fail_limit=%d", unauth, limited)
		}
		return fmt.Sprintf("%d неудач → 401; %d-я → 429 Retry-After=%ds; валидный ключ во время блокировки → 429; /healthz 200; аудит: %d unauthorized + 2 rate_limited/auth_fail_limit",
			maxFail, maxFail+1, ra, maxFail)
	})
}

func TestAuth10KeyNeverLeaks(t *testing.T) {
	extra(t, "AUTH-10", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/auth-10", nil)
		before := st.rec.Count()
		sent := 0
		for _, key := range []string{e2eKey1, e2eKey2} {
			for _, c := range st.cases {
				if st.realModel() && c.Category == "output" {
					continue
				}
				if c.ID != "IN-01" && c.ID != "IN-02" && c.ID != "OUT-01" {
					continue
				}
				if st.fake != nil {
					st.fake.Script(c.UpstreamReply, &Usage{Prompt: 40, Completion: 20})
				}
				h.chat(t, localModel, c.Messages, map[string]string{"Authorization": "Bearer " + key})
				sent++
			}
		}
		h.raw(t, http.MethodPost, "/v1/chat/completions?api_key="+e2eKey2, chatBody(), authHeader("Bearer "+e2eKey2))
		h.raw(t, http.MethodPost, "/v1/chat/completions", chatBody(), authHeader("Basic "+e2eKey1))
		if st.rec.Count()-before != sent {
			t.Fatalf("upstream calls %d, want %d", st.rec.Count()-before, sent)
		}
		allowed := map[string]bool{"": true}
		if st.upstream == upstreamFake {
			allowed["Bearer e2e-fake-key"] = true
		}
		for i, a := range st.rec.Auths() {
			if !allowed[a] || strings.Contains(a, e2eKey1) || strings.Contains(a, e2eKey2) {
				t.Fatalf("upstream request %d had Authorization of length %d (not the upstream key)", i, len(a))
			}
		}
		for _, b := range st.rec.Since(0) {
			if strings.Contains(b, e2eKey1) || strings.Contains(b, e2eKey2) {
				t.Fatal("proxy key in an upstream body")
			}
		}
		logs, audits := allFiles("proxy.log"), allFiles("audit.jsonl")
		for _, f := range append(append([]string(nil), logs...), audits...) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for name, v := range forbiddenEverywhere() {
				if strings.Contains(string(data), v) {
					t.Errorf("%s contains %s", f, name)
				}
			}
		}
		st.mu.Lock()
		rs, ex := append([]Result(nil), st.results...), append([]ExtraResult(nil), st.extras...)
		st.mu.Unlock()
		html, err := RenderRunHTML(Meta{SchemaVersion: ReportSchemaVersion, Upstream: st.upstream, OutDir: st.outDir}, st.cases, rs, ex, audits)
		if err != nil {
			t.Fatal(err)
		}
		for name, v := range forbiddenEverywhere() {
			if strings.Contains(html, v) {
				t.Errorf("HTML contains %s", name)
			}
		}
		return fmt.Sprintf("ключи прокси не найдены: upstream (%d заголовков: пусто или ключ upstream), %d slog, %d audit, HTML-снимок", len(st.rec.Auths()), len(logs), len(audits))
	})
}
