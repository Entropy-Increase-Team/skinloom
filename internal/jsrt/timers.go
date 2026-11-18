package jsrt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/fastschema/qjs"
)

// installSandboxGlobals strips QuickJS native OS/FS globals and installs bounded
// Web-standard APIs (TextEncoder, TextDecoder, URL, URLSearchParams, crypto,
// context-bounded setTimeout/clearTimeout, and slog-backed console).
func installSandboxGlobals(inst *qjsInstance, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	ctx := inst.rt.Context()

	ctx.SetFunc("__jsrt_console", hostWrap(inst, func(level, msg string) {
		switch level {
		case "debug":
			log.Debug(msg, "source", "plugin")
		case "warn":
			log.Warn(msg, "source", "plugin")
		case "error":
			log.Error(msg, "source", "plugin")
		default:
			log.Info(msg, "source", "plugin")
		}
	}))

	ctx.SetFunc("__jsrt_random_bytes", hostWrapErr(inst, func(n int) ([]byte, error) {
		if n < 0 || n > 65536 {
			return nil, fmt.Errorf("getRandomValues length must be between 0 and 65536 bytes")
		}
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		return buf, nil
	}))

	ctx.SetFunc("__jsrt_random_uuid", hostWrapErr(inst, func() (string, error) {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%s-%s-%s-%s-%s",
			hex.EncodeToString(b[0:4]),
			hex.EncodeToString(b[4:6]),
			hex.EncodeToString(b[6:8]),
			hex.EncodeToString(b[8:10]),
			hex.EncodeToString(b[10:16]),
		), nil
	}))

	ctx.SetFunc("__jsrt_parse_url", hostWrapErr(inst, func(raw, base string) (map[string]any, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid URL: %w", err)
		}
		if base != "" {
			b, err := url.Parse(base)
			if err != nil {
				return nil, fmt.Errorf("invalid base URL: %w", err)
			}
			u = b.ResolveReference(u)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid URL: missing scheme or host")
		}
		pw, _ := u.User.Password()
		user := u.User.Username()
		search := ""
		if u.RawQuery != "" {
			search = "?" + u.RawQuery
		}
		hash := ""
		if u.Fragment != "" {
			hash = "#" + u.Fragment
		}
		pathname := u.EscapedPath()
		if pathname == "" {
			pathname = "/"
		}
		return map[string]any{
			"href":     u.String(),
			"origin":   strings.ToLower(u.Scheme) + "://" + u.Host,
			"protocol": strings.ToLower(u.Scheme) + ":",
			"username": user,
			"password": pw,
			"host":     u.Host,
			"hostname": u.Hostname(),
			"port":     u.Port(),
			"pathname": pathname,
			"search":   search,
			"hash":     hash,
		}, nil
	}))

	val, err := ctx.Eval("<jsrt-bootstrap>", qjs.Code(bootstrapJS()))
	if val != nil {
		val.Free()
	}
	return err
}

func bootstrapJS() string {
	return `(() => {
  const nativeSetTimeout = globalThis.os && globalThis.os.setTimeout;
  const nativeClearTimeout = globalThis.os && globalThis.os.clearTimeout;
  const activeTimers = new Set();
  const maxDelayMs = 30000;

  // Remove privileged QuickJS-NG host/stdio/fs globals from globalThis.
  delete globalThis.os;
  delete globalThis.std;
  delete globalThis.bjson;
  delete globalThis.setInterval;
  delete globalThis.clearInterval;
  delete globalThis.scriptArgs;
  delete globalThis.navigator;

  globalThis.__jsrt_clearTimers = () => {
    if (nativeClearTimeout) {
      for (const id of activeTimers) {
        try { nativeClearTimeout(id); } catch {}
      }
    }
    activeTimers.clear();
  };

  globalThis.setTimeout = (fn, delay = 0, ...args) => {
    if (typeof fn !== 'function') {
      throw new TypeError('setTimeout callback must be a function');
    }
    if (!nativeSetTimeout) {
      throw new Error('setTimeout unavailable in sandbox');
    }
    let ms = Number(delay);
    if (!Number.isFinite(ms) || ms < 0) ms = 0;
    if (ms > maxDelayMs) ms = maxDelayMs;
    const handle = {};
    const id = nativeSetTimeout(() => {
      activeTimers.delete(handle.id);
      fn(...args);
    }, ms);
    handle.id = id;
    activeTimers.add(id);
    return id;
  };

  globalThis.clearTimeout = (id) => {
    if (nativeClearTimeout && activeTimers.has(id)) {
      activeTimers.delete(id);
      nativeClearTimeout(id);
    }
  };

  const formatArgs = (args) => args.map((a) => {
    if (typeof a === 'string') return a;
    if (a instanceof Error) return a.stack || (a.name + ': ' + a.message);
    if (a === undefined) return 'undefined';
    if (a === null) return 'null';
    if (typeof a === 'object') {
      try { return JSON.stringify(a); } catch { return String(a); }
    }
    return String(a);
  }).join(' ');

  globalThis.console = {
    debug: (...args) => __jsrt_console('debug', formatArgs(args)),
    log:   (...args) => __jsrt_console('info', formatArgs(args)),
    info:  (...args) => __jsrt_console('info', formatArgs(args)),
    warn:  (...args) => __jsrt_console('warn', formatArgs(args)),
    error: (...args) => __jsrt_console('error', formatArgs(args)),
  };

  globalThis.TextEncoder = class TextEncoder {
    get encoding() { return 'utf-8'; }
    encode(str = '') {
      const s = String(str);
      const out = [];
      for (let i = 0; i < s.length; i++) {
        let cp = s.codePointAt(i);
        if (cp > 0xffff) i++;
        if (cp < 0x80) {
          out.push(cp);
        } else if (cp < 0x800) {
          out.push(0xc0 | (cp >> 6), 0x80 | (cp & 0x3f));
        } else if (cp < 0x10000) {
          out.push(0xe0 | (cp >> 12), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f));
        } else {
          out.push(0xf0 | (cp >> 18), 0x80 | ((cp >> 12) & 0x3f), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f));
        }
      }
      return new Uint8Array(out);
    }
  };

  globalThis.TextDecoder = class TextDecoder {
    constructor(label = 'utf-8') {
      const norm = String(label).trim().toLowerCase();
      if (norm !== 'utf-8' && norm !== 'utf8' && norm !== 'unicode-1-1-utf-8') {
        throw new RangeError('Unsupported encoding: ' + label);
      }
      this._encoding = 'utf-8';
    }
    get encoding() { return this._encoding; }
    decode(input) {
      if (input == null) return '';
      const bytes = input instanceof Uint8Array
        ? input
        : ArrayBuffer.isView(input)
          ? new Uint8Array(input.buffer, input.byteOffset, input.byteLength)
          : input instanceof ArrayBuffer
            ? new Uint8Array(input)
            : new Uint8Array(0);
      let out = '';
      for (let i = 0; i < bytes.length; ) {
        const b0 = bytes[i];
        if (b0 < 0x80) {
          out += String.fromCharCode(b0);
          i++;
        } else if ((b0 & 0xe0) === 0xc0 && i + 1 < bytes.length && (bytes[i + 1] & 0xc0) === 0x80) {
          const cp = ((b0 & 0x1f) << 6) | (bytes[i + 1] & 0x3f);
          out += cp >= 0x80 ? String.fromCharCode(cp) : '\uFFFD';
          i += 2;
        } else if ((b0 & 0xf0) === 0xe0 && i + 2 < bytes.length && (bytes[i + 1] & 0xc0) === 0x80 && (bytes[i + 2] & 0xc0) === 0x80) {
          const cp = ((b0 & 0x0f) << 12) | ((bytes[i + 1] & 0x3f) << 6) | (bytes[i + 2] & 0x3f);
          out += (cp >= 0x800 && !(cp >= 0xd800 && cp <= 0xdfff)) ? String.fromCharCode(cp) : '\uFFFD';
          i += 3;
        } else if ((b0 & 0xf8) === 0xf0 && i + 3 < bytes.length && (bytes[i + 1] & 0xc0) === 0x80 && (bytes[i + 2] & 0xc0) === 0x80 && (bytes[i + 3] & 0xc0) === 0x80) {
          const cp = ((b0 & 0x07) << 18) | ((bytes[i + 1] & 0x3f) << 12) | ((bytes[i + 2] & 0x3f) << 6) | (bytes[i + 3] & 0x3f);
          out += (cp >= 0x10000 && cp <= 0x10ffff) ? String.fromCodePoint(cp) : '\uFFFD';
          i += 4;
        } else {
          out += '\uFFFD';
          i++;
        }
      }
      return out;
    }
  };

  class URLSearchParams {
    constructor(init = '') {
      this._pairs = [];
      if (typeof init === 'string') {
        const s = init.startsWith('?') ? init.slice(1) : init;
        if (s !== '') {
          for (const part of s.split('&')) {
            const idx = part.indexOf('=');
            const k = idx === -1 ? part : part.slice(0, idx);
            const v = idx === -1 ? '' : part.slice(idx + 1);
            this._pairs.push([decodeURIComponent(k.replace(/\+/g, ' ')), decodeURIComponent(v.replace(/\+/g, ' '))]);
          }
        }
      } else if (Array.isArray(init)) {
        for (const pair of init) {
          if (!pair || pair.length < 2) throw new TypeError('Invalid pair');
          this._pairs.push([String(pair[0]), String(pair[1])]);
        }
      } else if (init && typeof init === 'object') {
        for (const [k, v] of Object.entries(init)) {
          this._pairs.push([String(k), String(v)]);
        }
      }
    }
    append(name, value) { this._pairs.push([String(name), String(value)]); }
    set(name, value) {
      const n = String(name), v = String(value);
      let replaced = false;
      this._pairs = this._pairs.filter(([k]) => {
        if (k !== n) return true;
        if (!replaced) { replaced = true; return true; }
        return false;
      }).map(([k, old]) => k === n ? [k, v] : [k, old]);
      if (!replaced) this._pairs.push([n, v]);
    }
    delete(name) { const n = String(name); this._pairs = this._pairs.filter(([k]) => k !== n); }
    get(name) { const n = String(name); const hit = this._pairs.find(([k]) => k === n); return hit ? hit[1] : null; }
    getAll(name) { const n = String(name); return this._pairs.filter(([k]) => k === n).map(([, v]) => v); }
    has(name) { const n = String(name); return this._pairs.some(([k]) => k === n); }
    *entries() { for (const p of this._pairs) yield p; }
    *keys() { for (const [k] of this._pairs) yield k; }
    *values() { for (const [, v] of this._pairs) yield v; }
    [Symbol.iterator]() { return this.entries(); }
    forEach(fn, thisArg) { for (const [k, v] of this._pairs) fn.call(thisArg, v, k, this); }
    toString() {
      return this._pairs
        .map(([k, v]) => encodeURIComponent(k) + '=' + encodeURIComponent(v))
        .join('&');
    }
  }
  globalThis.URLSearchParams = URLSearchParams;

  globalThis.URL = class URL {
    constructor(input, base = '') {
      const p = __jsrt_parse_url(String(input), base ? String(base) : '');
      this.href = p.href;
      this.origin = p.origin;
      this.protocol = p.protocol;
      this.username = p.username;
      this.password = p.password;
      this.host = p.host;
      this.hostname = p.hostname;
      this.port = p.port;
      this.pathname = p.pathname;
      this.search = p.search;
      this.hash = p.hash;
      this.searchParams = new URLSearchParams(p.search);
    }
    toString() {
      const qs = this.searchParams.toString();
      return this.origin + this.pathname + (qs ? '?' + qs : '') + this.hash;
    }
    toJSON() { return this.toString(); }
  };

  globalThis.crypto = {
    getRandomValues(typedArray) {
      if (!typedArray || !ArrayBuffer.isView(typedArray) || typedArray instanceof DataView) {
        throw new TypeError('Expected an integer TypedArray');
      }
      const bytes = __jsrt_random_bytes(typedArray.byteLength);
      const dst = new Uint8Array(typedArray.buffer, typedArray.byteOffset, typedArray.byteLength);
      dst.set(bytes);
      return typedArray;
    },
    randomUUID() {
      return __jsrt_random_uuid();
    },
  };
})();`
}
