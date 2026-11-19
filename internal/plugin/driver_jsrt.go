package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
)

// JSDriver executes Tier 1 TypeScript/JavaScript plugins inside a bounded
// QuickJS-NG/Wazero isolate pool.
type JSDriver struct {
	engine     jsrt.Engine
	bundleCode []byte

	rt     *pluginRuntime
	pool   *jsrt.Pool
	closed atomic.Bool
}

// NewJSDriver creates a JSDriver backed by engine for bundleCode.
func NewJSDriver(engine jsrt.Engine, bundleCode []byte) *JSDriver {
	return &JSDriver{
		engine:     engine,
		bundleCode: bundleCode,
	}
}

func (d *JSDriver) Init(ctx context.Context, cfg DriverInitConfig) (*DriverRegistrations, error) {
	d.rt = cfg.Runtime
	if len(d.bundleCode) == 0 {
		return &DriverRegistrations{}, nil
	}
	pool, err := jsrt.NewPool(d.engine, jsrt.PoolSpec{
		ID:       d.rt.id,
		Filename: d.rt.manifest.Server,
		Code:     d.bundleCode,
		Limits: jsrt.Limits{
			PoolSize:     jsrt.DefaultPoolSize,
			MemoryLimit:  jsrt.DefaultMemoryLimit,
			MaxStackSize: jsrt.DefaultMaxStackSize,
			Timeout:      defaultGuardTimeout,
		},
		Log:     d.rt.log,
		Prepare: d.prepareInstance,
		OnSetup: func(setupCtx context.Context, inst jsrt.Instance) error {
			return d.runSetupInInstance(setupCtx, inst, true)
		},
		OnWarm: func(warmCtx context.Context, inst jsrt.Instance) error {
			return d.runSetupInInstance(warmCtx, inst, false)
		},
	})
	if err != nil {
		return nil, err
	}
	d.pool = pool

	d.rt.setupMu.Lock()
	regs := &DriverRegistrations{
		Hooks:               append([]hookBinding(nil), d.rt.collectedHooks...),
		Routes:              append([]routeBinding(nil), d.rt.collectedRoute...),
		AuthProviders:       append([]authProviderBinding(nil), d.rt.authProviders...),
		PasswordVerifiers:   append([]passwordVerifierBinding(nil), d.rt.verifiers...),
		CaptchaProviders:    append([]captchaBinding(nil), d.rt.captchas...),
		CDNProviders:        append([]cdnBinding(nil), d.rt.cdns...),
		YggdrasilMetas:      append([]yggMetaBinding(nil), d.rt.yggMetas...),
		Crons:               append([]cronBinding(nil), d.rt.crons...),
		Services:            append([]serviceBinding(nil), d.rt.services...),
		HasSettingsOnChange: d.rt.hasOnChange,
	}
	d.rt.setupMu.Unlock()
	return regs, nil
}

func (d *JSDriver) runSetupInInstance(ctx context.Context, inst jsrt.Instance, collect bool) error {
	r := d.rt
	r.setupMu.Lock()
	r.collecting = collect
	if collect {
		r.collectedHooks = nil
		r.collectedRoute = nil
		r.authProviders = nil
		r.verifiers = nil
		r.captchas = nil
		r.cdns = nil
		r.yggMetas = nil
		r.crons = nil
		r.services = nil
		r.hasOnChange = false
	}
	r.setupMu.Unlock()
	defer func() {
		r.setupMu.Lock()
		r.collecting = false
		r.setupMu.Unlock()
	}()

	setupCtx, cancel := context.WithTimeout(ctx, defaultHookTimeout)
	defer cancel()
	_, _, err := inst.InvokeGlobal(setupCtx, "__sl_run_setup")
	return err
}

func (d *JSDriver) prepareInstance(_ context.Context, inst jsrt.Instance) error {
	r := d.rt
	bindings := map[string]jsrt.HostFunc{
		"__sl_setup_hook":     r.hostRegisterHook,
		"__sl_setup_route":    r.hostRegisterRoute,
		"__sl_setup_onChange": r.hostRegisterOnChange,
		"__sl_event_emit":     r.hostEmitEvent,
		"__sl_kv":             r.hostKV,
		"__sl_db":             r.hostDB,
		"__sl_files":          r.hostFiles,
		"__sl_fetch":          r.hostFetch,
		"__sl_settings":       r.hostSettings,
		"__sl_site":           r.hostSite,
		"__sl_users":          r.hostUsers,
		"__sl_players":        r.hostPlayers,
		"__sl_textures":       r.hostTextures,
		"__sl_closet":         r.hostCloset,
		"__sl_score":          r.hostScore,
		"__sl_notify":         r.hostNotify,
		"__sl_mail":           r.hostMail,
		"__sl_auth":           r.hostAuth,
		"__sl_captcha":        r.hostCaptcha,
		"__sl_cdn":            r.hostCDN,
		"__sl_yggdrasil":      r.hostYggdrasil,
		"__sl_cron":           r.hostCron,
		"__sl_services":       r.hostServices,
		"__sl_audit":          r.hostAudit,
		"__sl_crypto":         r.hostCrypto,
		"__sl_cache":          r.hostCache,
		"__sl_i18n":           r.hostI18n,
		"__sl_log":            r.hostLog,
	}
	for name, fn := range bindings {
		if err := inst.RegisterHostFunc(name, fn); err != nil {
			return err
		}
	}
	if _, _, err := inst.Eval(context.Background(), "<plugin-ctx-prelude>", pluginPreludeJS()); err != nil {
		return err
	}
	return nil
}

func (d *JSDriver) invokeJS(ctx context.Context, timeout time.Duration, globalFn string, args ...any) (any, error) {
	if d.closed.Load() || d.pool == nil {
		return nil, errors.New("plugin runtime not active")
	}
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, timeout)
		defer cancel()
	}

	var out any
	_, err := d.pool.Do(callCtx, func(inst jsrt.Instance) (jsrt.CallStats, error) {
		val, stats, err := inst.InvokeGlobal(callCtx, globalFn, args...)
		out = val
		return stats, err
	})
	if err != nil {
		if d.rt != nil {
			d.rt.recordRuntimeFailure(err)
		}
		return nil, err
	}
	if d.rt != nil {
		d.rt.circuit.RecordSuccess()
	}
	return out, nil
}

func parseJSHandlerID(handlerID string) (int, error) {
	fnID, err := strconv.Atoi(handlerID)
	if err != nil {
		return 0, fmt.Errorf("invalid JS handler id %q: %w", handlerID, err)
	}
	return fnID, nil
}

func (d *JSDriver) Lifecycle(ctx context.Context, method string, payload any) error {
	if d.pool == nil || d.closed.Load() {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, defaultHookTimeout)
	defer cancel()
	_, err := d.pool.Do(callCtx, func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, stats, err := inst.InvokeGlobal(callCtx, "__sl_run_lifecycle", method, payload)
		return stats, err
	})
	return err
}

func (d *JSDriver) InvokeGuard(ctx context.Context, handlerID, _ string, payload any) error {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return err
	}
	res, err := d.invokeJS(ctx, defaultGuardTimeout, "__sl_invoke_handler", fnID, payload)
	if err != nil {
		return err
	}
	if m, ok := res.(map[string]any); ok {
		if rejected, _ := m["__skinloomReject"].(bool); rejected {
			reason, _ := m["reason"].(string)
			if reason == "" {
				reason = "rejected by plugin " + d.rt.id
			}
			return hook.Reject(reason)
		}
	}
	return nil
}

func (d *JSDriver) InvokeFilter(ctx context.Context, handlerID, _ string, value, payload any) (any, error) {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return nil, err
	}
	return d.invokeJS(ctx, defaultGuardTimeout, "__sl_invoke_handler", fnID, value, payload)
}

func (d *JSDriver) InvokeEvent(ctx context.Context, handlerID, _ string, payload any) error {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return err
	}
	_, err = d.invokeJS(ctx, defaultHookTimeout, "__sl_invoke_handler", fnID, payload)
	return err
}

func (d *JSDriver) InvokeHTTP(ctx context.Context, handlerID string, req map[string]any) (any, error) {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return nil, err
	}
	return d.invokeJS(ctx, defaultRouteTimeout, "__sl_invoke_handler", fnID, req)
}

func (d *JSDriver) InvokeCron(ctx context.Context, handlerID, _ string) error {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return err
	}
	_, err = d.invokeJS(ctx, defaultCronTimeout, "__sl_invoke_handler", fnID)
	return err
}

func (d *JSDriver) InvokeService(ctx context.Context, handlerID, _, _ string, args ...any) (any, error) {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return nil, err
	}
	callArgs := append([]any{fnID}, args...)
	return d.invokeJS(ctx, defaultHookTimeout, "__sl_invoke_handler", callArgs...)
}

func (d *JSDriver) InvokeExtension(ctx context.Context, kind, _, handlerID string, args ...any) (any, error) {
	fnID, err := parseJSHandlerID(handlerID)
	if err != nil {
		return nil, err
	}
	timeout := defaultHookTimeout
	if kind == "captcha" || kind == "yggdrasil_meta" {
		timeout = defaultGuardTimeout
	}
	callArgs := append([]any{fnID}, args...)
	return d.invokeJS(ctx, timeout, "__sl_invoke_handler", callArgs...)
}

func (d *JSDriver) NotifySettings(ctx context.Context, newSettings map[string]any) error {
	if d.pool == nil || d.closed.Load() {
		return nil
	}
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(callCtx, defaultHookTimeout)
	defer cancel()
	err := d.pool.Broadcast(callCtx, func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, stats, callErr := inst.InvokeGlobal(callCtx, "__sl_fire_settings_change", newSettings)
		return stats, callErr
	})
	if err != nil {
		if d.rt != nil {
			d.rt.recordRuntimeFailure(err)
		}
		return err
	}
	if d.rt != nil {
		d.rt.circuit.RecordSuccess()
	}
	return nil
}

func (d *JSDriver) ProxyHandler() http.Handler {
	return nil
}

func (d *JSDriver) Stats() DriverStats {
	out := DriverStats{Runtime: RuntimeJS}
	if d.pool != nil {
		st := d.pool.Stats()
		out.Pool = &st
	}
	return out
}

func (d *JSDriver) DrainAndClose(timeout time.Duration) error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}
	if d.pool != nil {
		return d.pool.DrainAndClose(timeout)
	}
	return nil
}

var _ Driver = (*JSDriver)(nil)

func pluginPreludeJS() string {
	return `(() => {
  const handlers = [];
  const settingsListeners = [];
  let nextFnId = 0;

  function registerHandler(fn) {
    const id = nextFnId++;
    handlers[id] = fn;
    return id;
  }

  function decodeBase64(b64) {
    const table = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
    const clean = String(b64 || '').replace(/[^A-Za-z0-9+/=]/g, '');
    const len = clean.length;
    if (len === 0) return new Uint8Array(0);
    let pad = 0;
    if (clean[len - 1] === '=') pad++;
    if (clean[len - 2] === '=') pad++;
    const outLen = ((len * 3) >> 2) - pad;
    const out = new Uint8Array(outLen);
    let p = 0;
    for (let i = 0; i < len; i += 4) {
      const a = table.indexOf(clean[i]);
      const b = table.indexOf(clean[i + 1]);
      const c = clean[i + 2] === '=' ? 0 : table.indexOf(clean[i + 2]);
      const d = clean[i + 3] === '=' ? 0 : table.indexOf(clean[i + 3]);
      const n = (a << 18) | (b << 12) | (c << 6) | d;
      if (p < outLen) out[p++] = (n >> 16) & 255;
      if (p < outLen) out[p++] = (n >> 8) & 255;
      if (p < outLen) out[p++] = n & 255;
    }
    return out;
  }

  globalThis.reject = (reason) => ({
    __skinloomReject: true,
    reason: String(reason ?? 'rejected'),
  });

  function wrapHttpMethod(method) {
    return (path, optsOrFn, maybeFn) => {
      let opts = {};
      let fn = optsOrFn;
      if (typeof optsOrFn !== 'function') {
        opts = optsOrFn || {};
        fn = maybeFn;
      }
      if (typeof fn !== 'function') {
        throw new TypeError('HTTP route handler must be a function');
      }
      const id = registerHandler(fn);
      return __sl_setup_route(method, String(path), String(opts.auth || 'public'), id);
    };
  }

  const ctx = {
    hooks: {
      reject: globalThis.reject,
      guard(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('guard handler must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('guard', String(name), Number(order) || 0, id);
      },
      filter(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('filter handler must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('filter', String(name), Number(order) || 0, id);
      },
    },
    events: {
      on(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('event listener must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('event', String(name), Number(order) || 0, id);
      },
      emit(localName, payload) {
        return __sl_event_emit(String(localName), payload ?? null);
      },
    },
    http: {
      get: wrapHttpMethod('GET'),
      post: wrapHttpMethod('POST'),
      put: wrapHttpMethod('PUT'),
      patch: wrapHttpMethod('PATCH'),
      delete: wrapHttpMethod('DELETE'),
      route(method, path, optsOrFn, maybeFn) {
        return wrapHttpMethod(String(method).toUpperCase())(path, optsOrFn, maybeFn);
      },
    },
    kv: {
      get: (key) => __sl_kv('get', String(key)),
      set: (key, value, ttlSeconds = 0) => __sl_kv('set', String(key), value ?? null, Number(ttlSeconds) || 0),
      delete: (key) => __sl_kv('delete', String(key)),
      list: (prefix = '', limit = 100) => __sl_kv('list', String(prefix), Number(limit) || 100),
      incr: (key, delta = 1) => __sl_kv('incr', String(key), delta === undefined ? 1 : Number(delta)),
    },
    db: {
      collection(name) {
        const c = String(name);
        return {
          insertOne: (doc) => __sl_db('insertOne', c, doc || {}),
          findOne: (filter = {}) => __sl_db('findOne', c, filter || {}),
          find: (filter = {}, opts = {}) => __sl_db('find', c, filter || {}, opts || {}),
          updateOne: (filter, patch) => __sl_db('updateOne', c, filter || {}, patch || {}),
          updateMany: (filter, patch) => __sl_db('updateMany', c, filter || {}, patch || {}),
          deleteOne: (filter) => __sl_db('deleteOne', c, filter || {}),
          count: (filter = {}) => __sl_db('count', c, filter || {}),
        };
      },
    },
    files: {
      put: (key, data) => __sl_files('put', String(key), data),
      get: (key) => __sl_files('get', String(key)),
      delete: (key) => __sl_files('delete', String(key)),
      url: (key) => __sl_files('url', String(key)),
    },
    async fetch(url, init = {}) {
      const resp = await __sl_fetch(String(url), {
        method: init?.method || 'GET',
        headers: init?.headers || {},
        body: init?.body != null ? String(init.body) : '',
        timeoutMs: Number(init?.timeoutMs) || 0,
      });
      return {
        ok: Boolean(resp.ok),
        status: resp.status,
        statusText: resp.statusText,
        headers: resp.headers || {},
        url: resp.url,
        async text() { return resp.bodyText ?? ''; },
        async json() { return JSON.parse(resp.bodyText ?? 'null'); },
        async arrayBuffer() { return decodeBase64(resp.bodyBase64).buffer; },
      };
    },
    settings: {
      get: async (key) => {
        const v = await __sl_settings('get', String(key));
        return v === null ? undefined : v;
      },
      all: () => __sl_settings('all'),
      onChange(fn) {
        if (typeof fn !== 'function') throw new TypeError('onChange expects a function');
        settingsListeners.push(fn);
        return __sl_setup_onChange();
      },
    },
    site: {
      name: (locale = '') => __sl_site('name', String(locale || '')),
      url: () => __sl_site('url'),
      description: (locale = '') => __sl_site('description', String(locale || '')),
      locales: () => __sl_site('locales'),
      publicSettings: () => __sl_site('public'),
    },
    crypto: {
      randomString: (len = 16) => __sl_crypto('randomString', Number(len) || 16),
      randomBytes: (len) => __sl_crypto('randomBytes', Number(len) || 0),
      sha256: (data) => __sl_crypto('sha256', data),
      hmac: (algo, key, data) => __sl_crypto('hmac', String(algo || 'sha256'), key, data),
      timingSafeEqual: (a, b) => __sl_crypto('timingSafeEqual', a, b),
      uuid: () => __sl_crypto('uuid'),
    },
    t: (key, params, locale = '') => __sl_i18n('t', String(key), params || null, String(locale || '')),
    i18n: {
      t: (key, params, locale = '') => __sl_i18n('t', String(key), params || null, String(locale || '')),
      locales: () => __sl_i18n('locales'),
    },
    log: {
      debug: (...a) => __sl_log('debug', a.map(String).join(' ')),
      info: (...a) => __sl_log('info', a.map(String).join(' ')),
      warn: (...a) => __sl_log('warn', a.map(String).join(' ')),
      error: (...a) => __sl_log('error', a.map(String).join(' ')),
    },
    cache: {
      get: (key) => __sl_cache('get', String(key)),
      set: (key, val, ttlSeconds = 0) => __sl_cache('set', String(key), val ?? null, Number(ttlSeconds) || 0),
      delete: (key) => __sl_cache('delete', String(key)),
    },
    users: {
      get: (id) => __sl_users('get', Number(id)),
      find: (email) => __sl_users('find', String(email)),
      list: (opts = {}) => __sl_users('list', opts || {}),
      update: (id, patch) => __sl_users('update', Number(id), patch || {}),
      ban: (id, banned = true) => __sl_users('ban', Number(id), Boolean(banned)),
    },
    players: {
      get: (id) => __sl_players('get', Number(id)),
      findByName: (name) => __sl_players('findByName', String(name)),
      list: (userId) => __sl_players('list', Number(userId)),
      create: (userId, name, form = null) => __sl_players('create', Number(userId), String(name), form || null),
      rename: (userId, playerId, name) => __sl_players('rename', Number(userId), Number(playerId), String(name)),
      setTextures: (userId, playerId, textures = {}) => __sl_players('setTextures', Number(userId), Number(playerId), textures || {}),
      delete: (userId, playerId, refund = true) => __sl_players('delete', Number(userId), Number(playerId), Boolean(refund)),
    },
    textures: {
      get: (id) => __sl_textures('get', Number(id)),
      list: (opts = {}) => __sl_textures('list', opts || {}),
      create: (input) => {
        const inp = input || {};
        if (inp.data && (inp.data instanceof Uint8Array || inp.data instanceof ArrayBuffer || ArrayBuffer.isView(inp.data))) {
          const { data, ...rest } = inp;
          return __sl_textures('create', rest, data);
        }
        return __sl_textures('create', inp);
      },
      update: (id, actorOrPatch, maybePatch) =>
        __sl_textures('update', Number(id), (typeof actorOrPatch === 'object' && actorOrPatch !== null ? actorOrPatch : maybePatch) || {}),
      delete: (id, actorOrRefund = false, maybeRefund = false) =>
        __sl_textures('delete', Number(id), Boolean(typeof actorOrRefund === 'boolean' ? actorOrRefund : maybeRefund)),
      readBytes: (id) => __sl_textures('readBytes', Number(id)),
    },
    closet: {
      list: (userId, opts = {}) => __sl_closet('list', Number(userId), opts || {}),
      add: (userId, textureId, name = '') => __sl_closet('add', Number(userId), Number(textureId), String(name || '')),
      rename: (userId, textureId, name) => __sl_closet('rename', Number(userId), Number(textureId), String(name)),
      remove: (userId, textureId, refund = true) => __sl_closet('remove', Number(userId), Number(textureId), Boolean(refund)),
    },
    score: {
      balance: (userId) => __sl_score('balance', Number(userId)),
      apply: (userId, delta, reason = '', refType = '', refId = '') =>
        __sl_score('apply', Number(userId), Number(delta), String(reason || ''), String(refType || ''), String(refId || '')),
    },
    notify: {
      send: (userIds, title, markdown) => __sl_notify('send', userIds, String(title), String(markdown ?? '')),
      announce: (audience, title, markdown) => __sl_notify('announce', String(audience || 'all'), String(title), String(markdown ?? '')),
    },
    mail: {
      send: (opts) => __sl_mail(opts || {}),
    },
    auth: {
      registerProvider: (def, fn) => {
        const fnId = typeof fn === 'function' ? registerHandler(fn) : null;
        return __sl_auth('registerProvider', def || {}, fnId);
      },
      registerPasswordVerifier: (algo, fn) => {
        if (typeof fn !== 'function') throw new TypeError('password verifier must be a function');
        return __sl_auth('registerPasswordVerifier', String(algo), registerHandler(fn));
      },
    },
    captcha: {
      registerProvider: (def, fn) => {
        if (typeof fn !== 'function') throw new TypeError('captcha verifier must be a function');
        return __sl_captcha('registerProvider', def || {}, registerHandler(fn));
      },
    },
    cdn: {
      registerPurger: (def, fn) => {
        if (typeof fn !== 'function') throw new TypeError('CDN purger must be a function');
        return __sl_cdn('registerPurger', def || {}, registerHandler(fn));
      },
    },
    yggdrasil: {
      publicKey: () => __sl_yggdrasil('publicKey'),
      lookupName: (name) => __sl_yggdrasil('lookupName', String(name)),
      lookupProfile: (uuid, base = '') => __sl_yggdrasil('lookupProfile', String(uuid), String(base || '')),
      extendMeta: (ext) => __sl_yggdrasil('extendMeta', ext || {}),
      registerMetaProvider: (fn) => {
        if (typeof fn !== 'function') throw new TypeError('meta provider must be a function');
        return __sl_yggdrasil('registerMetaProvider', registerHandler(fn));
      },
    },
    cron: {
      schedule: (name, expr, fn) => {
        if (typeof fn !== 'function') throw new TypeError('cron handler must be a function');
        return __sl_cron('schedule', String(name), String(expr), registerHandler(fn));
      },
    },
    services: {
      expose: (name, fn) => {
        if (typeof fn !== 'function') throw new TypeError('exposed service must be a function');
        return __sl_services('expose', String(name), registerHandler(fn));
      },
      use: (pluginId, serviceName) => (...callArgs) =>
        __sl_services('call', String(pluginId), String(serviceName), callArgs),
    },
    audit: {
      record: (action, targetType = '', targetId = '', meta = null) =>
        __sl_audit(String(action), String(targetType || ''), String(targetId || ''), meta || null),
    },
  };

  globalThis.fetch = ctx.fetch;
  globalThis.definePlugin = (def) => def;

  globalThis.__sl_run_setup = async () => {
    const mod = globalThis.__jsrt_module_default;
    if (!mod) {
      return;
    }
    if (typeof mod === 'function') {
      await mod(ctx);
      return;
    }
    if (typeof mod.setup === 'function') {
      await mod.setup(ctx);
    }
  };

  globalThis.__sl_invoke_handler = async (id, ...args) => {
    const fn = handlers[id];
    if (typeof fn !== 'function') {
      throw new Error('missing plugin handler #' + id);
    }
    return await fn(...args);
  };

  globalThis.__sl_run_lifecycle = async (method, arg) => {
    const mod = globalThis.__jsrt_module_default;
    if (!mod || typeof mod[method] !== 'function') {
      return null;
    }
    if (arg !== undefined && arg !== null) {
      return await mod[method](ctx, arg);
    }
    return await mod[method](ctx);
  };

  globalThis.__sl_fire_settings_change = async (nextSettings) => {
    for (const fn of settingsListeners) {
      await fn(nextSettings);
    }
  };
})();`
}
