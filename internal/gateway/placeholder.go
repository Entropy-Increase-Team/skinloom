package gateway

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/httpx"
)

// WebStatus describes the supervised frontend's lifecycle state for the waiting page.
type WebStatus struct {
	Mode      string    // managed | dev | external | off | ""
	State     string    // idle | starting | restarting | stopped | ""
	Attempts  int       // number of launches in the current Start session
	Failures  int       // consecutive failures since Start or last healthy run
	StartedAt time.Time // when Start was called
	LastErr   string    // last unexpected exit or launch error, if restarting
	NextRetry time.Time // scheduled next restart time while waiting in backoff
	Output    []string  // recent stdout/stderr lines from the last failed run
}

// SetWebStatus registers a callback that reports the current frontend startup status
// so the waiting page can show mode- and state-aware feedback.
func (g *Gateway) SetWebStatus(f func() WebStatus) {
	if f == nil {
		g.webStatus.Store(nil)
		return
	}
	g.webStatus.Store(&f)
}

func (g *Gateway) currentWebStatus() WebStatus {
	if f := g.webStatus.Load(); f != nil {
		return (*f)()
	}
	return WebStatus{}
}

func (g *Gateway) servePlaceholder(w http.ResponseWriter, r *http.Request, status int, reason string, starting bool) {
	if !wantsHTML(r) {
		serveNonHTMLPlaceholder(w, r, status, reason, starting)
		return
	}

	ws := WebStatus{}
	if starting {
		ws = g.currentWebStatus()
	}

	zh := prefersChinese(r)
	data := buildPlaceholderData(zh, reason, starting, ws)

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	if starting {
		data.Nonce = randomNonce()
		h.Set("Retry-After", "2")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+data.Nonce+"'; connect-src 'self'; frame-ancestors 'self'")
	} else {
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'")
	}
	httpx.SecurityHeaders(h)
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = placeholderTmpl.Execute(w, data)
}

func serveNonHTMLPlaceholder(w http.ResponseWriter, r *http.Request, status int, reason string, starting bool) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if starting {
		h.Set("Retry-After", "2")
	}
	httpx.SecurityHeaders(h)
	if wantsJSON(r) {
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		if r.Method == http.MethodHead {
			return
		}
		code := "frontend_unavailable"
		if starting {
			code = "frontend_starting"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   code,
			"message": reason,
		})
		return
	}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(reason + "\n"))
}

// wantsHTML reports whether r expects an HTML page rather than a machine-readable or
// subresource response (such as fetch/XHR JSON, JS chunks, CSS, or images).
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest") {
		return false
	}
	if dest := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest"))); dest != "" {
		switch dest {
		case "document", "iframe":
			return true
		default:
			return false
		}
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" {
		return true
	}
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml") || strings.Contains(accept, "*/*")
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json")
}

// prefersChinese checks Accept-Language and defaults to Chinese when unset.
func prefersChinese(r *http.Request) bool {
	al := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language")))
	if al == "" {
		return true
	}
	zhIdx := strings.Index(al, "zh")
	enIdx := strings.Index(al, "en")
	if zhIdx == -1 {
		return false
	}
	if enIdx == -1 {
		return true
	}
	return zhIdx < enIdx
}

func randomNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawStdEncoding.EncodeToString(b[:])
}

type placeholderData struct {
	Lang           string
	Starting       bool
	Restarting     bool
	IsDev          bool
	Nonce          string
	InitialElapsed int
	Title          string
	Subtitle       string
	Detail         string
	BackendBadge   string
	FrontendBadge  string
	WaitingLabel   string
	LoadingLabel   string
	ReloadLabel    string
	HintTitle      string
	HintBody       string
	NoScriptHint   string
}

func buildPlaceholderData(zh bool, reason string, starting bool, ws WebStatus) placeholderData {
	d := placeholderData{
		Lang:       "en",
		Starting:   starting,
		Restarting: starting && ws.State == "restarting",
		IsDev:      starting && ws.Mode == "dev",
	}
	if zh {
		d.Lang = "zh-CN"
	}
	if starting && !ws.StartedAt.IsZero() {
		sec := int(time.Since(ws.StartedAt).Seconds())
		if sec > 0 && sec < 3600 {
			d.InitialElapsed = sec
		}
	}

	if zh {
		d.BackendBadge = "后端运行正常"
		d.ReloadLabel = "立即刷新"
		d.NoScriptHint = "当前浏览器未启用脚本，页面将每 3 秒自动刷新。"
		if !starting {
			d.Title = "前端服务未运行"
			d.Subtitle = "Frontend Unavailable"
			d.Detail = reason
			d.FrontendBadge = "前端未启用"
			return d
		}
		if d.Restarting {
			d.Title = "前端正在重启"
			d.Subtitle = "Restarting Web Frontend"
			d.Detail = "前端进程异常退出，正在自动重新启动。若持续无法恢复，请查看服务端终端日志。"
			d.FrontendBadge = "前端重启中"
		} else if d.IsDev {
			d.Title = "前端开发环境启动中"
			d.Subtitle = "Starting Dev Frontend"
			d.Detail = "开发模式首次启动与路由编译通常需要 10–30 秒，就绪后将自动进入页面。"
			d.FrontendBadge = "开发模式启动中"
		} else {
			d.Title = "前端正在启动"
			d.Subtitle = "Starting Web Frontend"
			d.Detail = "前端服务正在初始化，就绪后将自动进入页面，无需手动刷新。"
			d.FrontendBadge = "前端启动中"
		}
		d.WaitingLabel = "已等待"
		d.LoadingLabel = "前端已就绪，正在加载页面…"
		d.HintTitle = "等待时间较长？"
		d.HintBody = "请查看 skinloom 终端或服务日志，确认前端依赖与运行环境正常，或尝试手动刷新。"
		return d
	}

	d.BackendBadge = "Backend healthy"
	d.ReloadLabel = "Reload now"
	d.NoScriptHint = "JavaScript is disabled; this page refreshes every 3 seconds."
	if !starting {
		d.Title = "Frontend Unavailable"
		d.Subtitle = "前端服务未运行"
		d.Detail = reason
		d.FrontendBadge = "Frontend offline"
		return d
	}
	if d.Restarting {
		d.Title = "Restarting Web Frontend"
		d.Subtitle = "前端正在重启"
		d.Detail = "The frontend process exited unexpectedly and is restarting automatically. Check server logs if this persists."
		d.FrontendBadge = "Restarting"
	} else if d.IsDev {
		d.Title = "Starting Dev Frontend"
		d.Subtitle = "前端开发环境启动中"
		d.Detail = "Initial dev server startup and route compilation typically takes 10–30 seconds. This page will open automatically once ready."
		d.FrontendBadge = "Starting (dev)"
	} else {
		d.Title = "Starting Web Frontend"
		d.Subtitle = "前端正在启动"
		d.Detail = "The web frontend is initializing. This page will open automatically as soon as it is ready."
		d.FrontendBadge = "Starting"
	}
	d.WaitingLabel = "Elapsed"
	d.LoadingLabel = "Frontend ready, loading page…"
	d.HintTitle = "Taking longer than expected?"
	d.HintBody = "Check the skinloom terminal or service logs to verify the frontend runtime and dependencies, or try reloading manually."
	return d
}

var placeholderTmpl = template.Must(template.New("placeholder").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
{{- if .Starting}}
<noscript><meta http-equiv="refresh" content="3"></noscript>
{{- end}}
<title>SkinLoom — {{.Title}}</title>
<style>
:root{
  --bg:#f4f6fb;--card:#ffffff;--border:rgba(15,23,42,.08);--shadow:0 10px 30px rgba(15,23,42,.06);
  --fg:#0f172a;--muted:#475569;--subtle:#64748b;--pill-bg:#f1f5f9;--code-bg:#e2e8f0;
  --warp:#3057a3;--weft:#edb345;--cut:rgba(12,14,18,.14);
  --ok:#16a34a;--warn:#d97706;--err:#dc2626;--btn-bg:#0f172a;--btn-fg:#ffffff;
}
@media (prefers-color-scheme:dark){
  :root{
    --bg:#0b0f17;--card:#121826;--border:rgba(248,250,252,.09);--shadow:0 12px 36px rgba(0,0,0,.45);
    --fg:#f1f5f9;--muted:#94a3b8;--subtle:#64748b;--pill-bg:#1e293b;--code-bg:#1e293b;
    --warp:#85abf0;--weft:#eebb58;--cut:rgba(12,14,18,.28);
    --ok:#4ade80;--warn:#fbbf24;--err:#f87171;--btn-bg:#f1f5f9;--btn-fg:#0f172a;
  }
}
*{box-sizing:border-box}
body{
  margin:0;min-height:100vh;display:grid;place-items:center;padding:1.5rem;
  font-family:system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC",sans-serif;
  background:var(--bg);color:var(--fg);-webkit-font-smoothing:antialiased;
}
main{
  width:100%;max-width:28rem;padding:2rem 1.75rem;border-radius:1rem;
  background:var(--card);border:1px solid var(--border);box-shadow:var(--shadow);
  text-align:center;
}
.brand{display:inline-flex;align-items:center;gap:.625rem;margin-bottom:1.25rem}
.logo{width:2rem;height:2rem;flex-shrink:0}
.logo .warp{fill:var(--warp)}.logo .weft{fill:var(--weft)}.logo .cut{fill:var(--cut)}
{{- if .Starting}}
.logo{animation:breathe 2.4s ease-in-out infinite}
@keyframes breathe{0%,100%{transform:scale(1);opacity:1}50%{transform:scale(.95);opacity:.82}}
{{- end}}
.brand-name{font-size:1.05rem;font-weight:700;letter-spacing:-.01em}
h1{margin:0;font-size:1.25rem;font-weight:650;letter-spacing:-.01em}
.sub{margin:.25rem 0 0;font-size:.82rem;color:var(--subtle)}
.detail{margin:1rem 0 0;font-size:.92rem;line-height:1.6;color:var(--muted);word-break:break-word}
.bar{
  position:relative;height:.25rem;margin:1.35rem 0 1.25rem;border-radius:999px;
  background:var(--pill-bg);overflow:hidden;
}
.bar-fill{
  position:absolute;inset:0 auto 0 0;width:38%;border-radius:999px;
  background:linear-gradient(90deg,var(--warp),var(--weft));
  animation:slide 1.5s cubic-bezier(.4,0,.2,1) infinite;
}
.bar.ready .bar-fill{width:100%;animation:none;transition:width .3s ease}
@keyframes slide{0%{transform:translateX(-110%)}100%{transform:translateX(300%)}}
.badges{display:flex;flex-wrap:wrap;justify-content:center;gap:.5rem;margin-top:1.25rem}
.pill{
  display:inline-flex;align-items:center;gap:.4rem;padding:.3rem .65rem;
  border-radius:999px;background:var(--pill-bg);color:var(--muted);
  font-size:.78rem;text-decoration:none;border:1px solid transparent;
}
a.pill:hover{border-color:var(--border);color:var(--fg)}
.dot{width:.45rem;height:.45rem;border-radius:50%;flex-shrink:0}
.dot.ok{background:var(--ok)}
.dot.warn{background:var(--warn);animation:pulse 1.4s ease-in-out infinite}
.dot.err{background:var(--err)}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.4;transform:scale(.85)}}
.meta{margin-top:.9rem;font-size:.8rem;color:var(--subtle);min-height:1.2rem}
.hint{
  display:none;margin-top:1.1rem;padding:.85rem 1rem;border-radius:.65rem;
  background:var(--pill-bg);text-align:left;font-size:.82rem;line-height:1.55;color:var(--muted);
}
.hint.show{display:block}
.hint strong{display:block;margin-bottom:.25rem;color:var(--fg);font-size:.84rem}
.actions{margin-top:1rem;display:flex;justify-content:center;gap:.6rem}
button{
  appearance:none;border:0;border-radius:.5rem;padding:.45rem .9rem;
  background:var(--btn-bg);color:var(--btn-fg);font-size:.82rem;font-weight:550;cursor:pointer;
}
button:hover{opacity:.88}
.noscript{margin-top:.85rem;font-size:.78rem;color:var(--subtle)}
</style>
</head>
<body>
<main>
  <div class="brand">
    <svg class="logo" viewBox="0 0 32 32" aria-hidden="true">
      <defs>
        <clipPath id="wl"><rect x="8" y="4" width="5" height="24" rx="1.5"/></clipPath>
        <clipPath id="wr"><rect x="19" y="4" width="5" height="24" rx="1.5"/></clipPath>
        <clipPath id="wt"><rect x="4" y="8" width="24" height="5" rx="1.5"/></clipPath>
        <clipPath id="wb"><rect x="4" y="19" width="24" height="5" rx="1.5"/></clipPath>
      </defs>
      <rect x="8" y="4" width="5" height="24" rx="1.5" class="warp"/>
      <rect x="19" y="4" width="5" height="24" rx="1.5" class="warp"/>
      <rect x="19" y="13" width="5" height="1.25" class="cut" clip-path="url(#wr)"/>
      <rect x="8" y="24" width="5" height="1.25" class="cut" clip-path="url(#wl)"/>
      <rect x="4" y="8" width="24" height="5" rx="1.5" class="weft"/>
      <rect x="4" y="19" width="24" height="5" rx="1.5" class="weft"/>
      <rect x="13" y="8" width="1.25" height="5" class="cut" clip-path="url(#wt)"/>
      <rect x="24" y="19" width="1.25" height="5" class="cut" clip-path="url(#wb)"/>
      <rect x="8" y="4" width="5" height="11" rx="1.5" class="warp"/>
      <rect x="19" y="17" width="5" height="11" rx="1.5" class="warp"/>
    </svg>
    <span class="brand-name">云织 SkinLoom</span>
  </div>
  <h1 id="title">{{.Title}}</h1>
  <p class="sub">{{.Subtitle}}</p>
  <p class="detail" id="detail">{{.Detail}}</p>
{{- if .Starting}}
  <div class="bar" id="bar"><div class="bar-fill"></div></div>
{{- end}}
  <div class="badges">
    <a class="pill" href="/healthz" target="_blank" rel="noopener"><span class="dot ok"></span>{{.BackendBadge}}</a>
{{- if .Starting}}
    <span class="pill" id="fe-pill"><span class="dot {{if .Restarting}}err{{else}}warn{{end}}" id="fe-dot"></span><span id="fe-text">{{.FrontendBadge}}</span></span>
{{- else}}
    <span class="pill"><span class="dot err"></span>{{.FrontendBadge}}</span>
{{- end}}
  </div>
{{- if .Starting}}
  <p class="meta" id="meta" aria-live="polite"></p>
  <div class="hint" id="hint">
    <strong>{{.HintTitle}}</strong>
    <span>{{.HintBody}}</span>
    <div class="actions"><button type="button" id="reload-btn">{{.ReloadLabel}}</button></div>
  </div>
  <noscript><p class="noscript">{{.NoScriptHint}}</p></noscript>
{{- end}}
</main>
{{- if .Starting}}
<script nonce="{{.Nonce}}">
(function(){
  var zh = {{if eq .Lang "zh-CN"}}true{{else}}false{{end}};
  var elapsed = {{.InitialElapsed}};
  var waitingLabel = {{.WaitingLabel}};
  var loadingLabel = {{.LoadingLabel}};
  var meta = document.getElementById("meta");
  var hint = document.getElementById("hint");
  var bar = document.getElementById("bar");
  var feDot = document.getElementById("fe-dot");
  var feText = document.getElementById("fe-text");
  var btn = document.getElementById("reload-btn");
  var loading = false;
  var stopped = false;

  if (btn) btn.addEventListener("click", function(){ location.reload(); });

  function renderTime(){
    if (loading) return;
    meta.textContent = zh ? (waitingLabel + " " + elapsed + " 秒") : (waitingLabel + " " + elapsed + "s");
    if (elapsed >= 30 && hint) hint.classList.add("show");
  }
  renderTime();
  setInterval(function(){
    if (!stopped) { elapsed++; renderTime(); }
  }, 1000);

  function warmAndReload(){
    if (loading) return;
    loading = true;
    if (bar) bar.classList.add("ready");
    if (feDot) feDot.className = "dot ok";
    if (feText) feText.textContent = zh ? "前端已就绪" : "Frontend ready";
    meta.textContent = loadingLabel;
    fetch(location.href, {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Accept": "text/html" }
    }).then(function(res){
      if (res.status === 502 || res.status === 503) {
        loading = false;
        if (bar) bar.classList.remove("ready");
        if (feDot) feDot.className = "dot warn";
        setTimeout(poll, 1000);
        return;
      }
      stopped = true;
      location.reload();
    }).catch(function(){
      loading = false;
      setTimeout(poll, 1000);
    });
  }

  function poll(){
    if (stopped || loading) return;
    fetch("/readyz", { cache: "no-store" }).then(function(res){
      if (res.ok) {
        warmAndReload();
      } else {
        setTimeout(poll, 800);
      }
    }).catch(function(){
      setTimeout(poll, 1000);
    });
  }
  setTimeout(poll, 600);
})();
</script>
{{- end}}
</body>
</html>`))
