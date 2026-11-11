// Utilidades comunes de los sitios ficticios del Demo Day.
// Los sitios solo hablan con la API de control del propio módulo (mismo origen).

import { icon, hydrateIcons } from './icons.js';

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
export const clp = (n) => '$' + Math.round(n).toLocaleString('es-CL');
export const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

/** Página de acceso (A6) que vuelve a donde estaba el usuario. */
export const loginURL = (nivel) => '/acceso?' + new URLSearchParams({ next: location.pathname + location.search, ...(nivel ? { nivel } : {}) });

/** Ejecuta una acción de usuario final por la ruta pública del sitio. Un rechazo del negocio (503/504) es un resultado, no una excepción. */
export async function userAction(platform, action) {
  const t0 = performance.now();
  let res;
  try {
    res = await fetch(`/sitios/api/${platform}/${action}`, { method: 'POST' });
  } catch {
    return { ok: false, status: 0, error: 'Sin conexión con el servicio', ms: performance.now() - t0 };
  }
  // Sesión vencida (A6): se vuelve a pedir la clave en vez de mostrar un error del negocio.
  if (res.status === 401) {
    location.assign(loginURL());
    return { ok: false, status: 401, error: 'Sesión vencida', ms: performance.now() - t0 };
  }
  const body = await res.json().catch(() => ({}));
  // root_kind es el tipo de falla donde nació (p. ej. saturation en un servicio
  // interno); kind es el del BFF, que solo dice que falló aguas abajo.
  return {
    ok: res.ok && body.ok === true, status: res.status, body, error: body.error, component: body.component,
    kind: body.root_kind || body.kind, traceId: body.trace_id, receipt: body.receipt, ms: performance.now() - t0,
  };
}

/** Texto único de error: código, qué no se pudo hacer, referencia y servicio causante. */
export function errorText(result, what) {
  const code = result.status || 502;
  const reason = result.kind === 'saturation' ? 'hay mucha demanda'
    : code === 504 ? 'tardó demasiado'
      : code === 500 ? 'ocurrió un error inesperado'
        : 'no disponible';
  let msg = `Error ${code} · ${what} · ${reason}`;
  if (result.traceId) msg += ` · Ref. ${result.traceId.slice(0, 8)}`;
  if (result.component) msg += ` · ${result.component}`;
  return msg;
}

/** Esqueleto de carga de una parte del sitio. */
export function skeleton(el, rows = 3) {
  el.innerHTML = `<div class="skeleton" aria-busy="true" aria-label="Cargando"><span class="sk"></span>${Array.from({ length: Math.max(0, rows - 1) }, () => '<span class="sk"></span>').join('')}</div>`;
}

/** Estado vacío de una parte del sitio. */
export function emptyState(el, what) {
  el.innerHTML = `<div class="state-empty">${esc(what)}</div>`;
}

/** Error de una parte del sitio, con el formato común y «Reintentar». */
export function errorState(el, { what, result, retry }) {
  el.innerHTML = `<div class="state-error" role="alert"><strong>${esc(errorText(result, what))}</strong><button type="button" data-retry>Reintentar</button></div>`;
  const b = el.querySelector('[data-retry]');
  if (b && retry) b.addEventListener('click', retry);
}

export function toast(msg, kind = '') {
  let box = $('#toasts');
  if (!box) { box = document.createElement('div'); box.id = 'toasts'; document.body.append(box); }
  const t = document.createElement('div');
  t.className = 'toast ' + kind;
  t.textContent = msg;
  box.append(t);
  setTimeout(() => t.remove(), 4200);
}

/** Carrusel de diapositivas con puntos y flechas. */
export function carousel(root, ms = 6000) {
  const slides = $$('.slide', root);
  const dots = $('.dots', root);
  let i = 0, timer;
  dots.innerHTML = slides.map((_, k) => `<button aria-label="Ir a la diapositiva ${k + 1}"></button>`).join('');
  const show = (n) => {
    i = (n + slides.length) % slides.length;
    slides.forEach((s, k) => s.classList.toggle('on', k === i));
    $$('button', dots).forEach((d, k) => d.classList.toggle('on', k === i));
  };
  const loop = () => { clearInterval(timer); timer = setInterval(() => show(i + 1), ms); };
  $$('button', dots).forEach((d, k) => d.addEventListener('click', () => { show(k); loop(); }));
  $('.prev', root)?.addEventListener('click', () => { show(i - 1); loop(); });
  $('.next', root)?.addEventListener('click', () => { show(i + 1); loop(); });
  show(0); loop();
}

/** Franja que identifica al sitio como ficticio (salvaguarda del spec §2.12). */
export function fictionStrip() {
  const bar = document.createElement('div');
  bar.className = 'fiction-strip'; bar.setAttribute('role', 'region'); bar.setAttribute('aria-label', 'Aviso de sitio ficticio');
  bar.innerHTML = 'Sitio ficticio · demostración de Sentinel · no es una empresa real ni procesa datos reales';
  document.body.prepend(bar);
}

/* ── Modo Director: consola flotante y colapsable ──────────────────────── */

// Nombres unificados con la consola del Demo Day (plan §6).
const FAULTS = [
  ['Cerrar puerto', 'down', { mode: 'refused' }],
  ['Responder 503', 'down', { mode: '503' }],
  ['Lentitud 950 ms', 'degraded', { latency_ms: 950, jitter_ms: 100 }],
  ['Errores 30 %', 'errors', { error_rate: 0.3 }],
];

export function directorBar(platform) {
  const el = document.createElement('aside');
  el.className = 'director';
  el.innerHTML = `<button class="director-toggle" aria-expanded="false">${icon('clapperboard')} Modo Director</button><div class="director-body" hidden></div>`;
  document.body.append(el);
  const toggle = $('.director-toggle', el), body = $('.director-body', el);
  let timer;

  const call = async (method, path, payload) => {
    const r = await fetch('/control/v1' + path, { method, body: payload ? JSON.stringify(payload) : undefined });
    if (!r.ok && r.status !== 204) throw new Error((await r.json().catch(() => ({}))).error || 'HTTP ' + r.status);
  };
  const paint = async () => {
    const r = await fetch('/control/v1/platforms').catch(() => null);
    // Con la clave de sitios no se controla nada: el Modo Director pide la de la consola (A6).
    if (r && (r.status === 401 || r.status === 403)) {
      clearInterval(timer);
      body.innerHTML = `<p class="d-head">El Modo Director pide la clave de la consola. <a href="${esc(loginURL('consola'))}">Ingresar</a></p>`;
      return;
    }
    const all = r && r.ok ? await r.json().catch(() => []) : [];
    const p = Array.isArray(all) ? all.find((x) => x.platform === platform) : null;
    if (!p) { body.textContent = 'demo-lab no responde'; return; }
    toggle.dataset.state = p.state;
    body.innerHTML = `<p class="d-head">Estado: <b class="st-${p.state}">${p.state}</b>
      <button data-all>Restaurar todo</button></p>
      <table>${p.services.map((s) => `<tr>
        <td><i class="d-dot st-${s.state}"></i>${s.slug}</td>
        <td>${s.faults.map((f) => `<button data-rm="${f.id}">${f.type} ${icon('x', 'sm', 2.2)}</button>`).join('') || ''}</td>
        <td>${FAULTS.map((f, k) => `<button data-f="${s.slug}|${k}">${f[0]}</button>`).join('')}</td></tr>`).join('')}</table>`;
  };
  body.addEventListener('click', async (e) => {
    const b = e.target.closest('button'); if (!b) return;
    try {
      if (b.dataset.all !== undefined) await call('POST', `/platforms/${platform}/restore`);
      else if (b.dataset.rm) await call('DELETE', '/faults/' + b.dataset.rm);
      else if (b.dataset.f) {
        const [svc, k] = b.dataset.f.split('|'); const f = FAULTS[k];
        await call('POST', '/faults', { platform, service: svc, type: f[1], params: f[2] });
      }
    } catch (err) { toast(err.message, 'bad'); }
    paint();
  });
  toggle.addEventListener('click', () => {
    const open = body.hidden;
    body.hidden = !open; toggle.setAttribute('aria-expanded', String(open));
    clearInterval(timer);
    if (open) { paint(); timer = setInterval(paint, 3000); }
  });
  // Atajo de teclado: D abre o cierra la consola.
  addEventListener('keydown', (e) => { if (e.key.toLowerCase() === 'd' && !/input|textarea|select/i.test(e.target.tagName) && !e.ctrlKey && !e.metaKey) toggle.click(); });
}

export function boot(platform) {
  hydrateIcons();
  fictionStrip();
  directorBar(platform);
}
