import { GUIDE, GROUPS, OPERATION, TAB_GROUP } from "./guide.js";
import { icon, hydrateIcons } from "/sitios/shared/icons.js";

// Consola del Demo Day. Sin librerías ni compilación: la estructura se arma una
// vez y cada sondeo solo actualiza textos y clases, para no cerrar la fila
// desplegada ni borrar lo que se está escribiendo en el campo de tráfico.

const API = "/control/v1";
const PLATFORMS = ["mercado", "banco", "clinica"];
const PLATFORM_SHORT = { mercado: "Mercado", banco: "Banco", clinica: "Clínica" };
const STATE_LABEL = { ok: "sano", degradado: "degradado", caido: "caído" };
const TABS = new Set(["inicio", "mercado", "banco", "clinica", "actividad", "datos"]);
const CHECK_LABELS = {
  reachable: "alcance",
  login: "inicio de sesión",
  projects: "proyectos",
  alerts: "alertas",
  ingest: "ingesta",
  probe: "sondeo",
  nodes: "nodos del mapa",
  portal: "portal del cliente",
};

const ACTION_LABELS = {
  mercado: {
    browse: "buscar productos",
    cart_add: "agregar al carro",
    login: "iniciar sesión",
    checkout: "pagar el pedido",
  },
  banco: {
    login: "iniciar sesión",
    balance: "consultar el saldo",
    transfer: "transferir fondos",
  },
  clinica: {
    search: "buscar especialidad",
    login: "iniciar sesión",
    book_appointment: "reservar una hora",
    list_exams: "ver mis exámenes",
    download_exam: "descargar un examen",
  },
};

// Las cinco fallas que ofrece la consola. `guide` es el id en GUIDE y a la vez
// la clave del botón: data-fault="<plataforma>/<servicio>/<guide>".
const FAULTS = [
  {
    guide: "fault-503",
    label: "Responder 503",
    type: "down",
    params: { mode: "503" },
  },
  {
    guide: "fault-refused",
    label: "Cerrar puerto",
    type: "down",
    params: { mode: "refused" },
  },
  {
    guide: "fault-latency",
    label: "Lentitud 950 ms",
    type: "degraded",
    params: { latency_ms: 950, jitter_ms: 100 },
  },
  {
    guide: "fault-errors",
    label: "Errores 30 %",
    type: "errors",
    params: { error_rate: 0.3 },
  },
  {
    guide: "fault-saturation",
    label: "Saturar",
    type: "saturation",
    params: { max_concurrency: 2 },
  },
];

const state = {
  tab: "inicio",
  status: null,
  statusOk: false,
  platforms: [],
  kinds: [],
  attacks: [],
  scripts: [],
  runs: [],
  verify: null,
  verifyAt: 0,
  verifying: false,
  warmupSince: null,
  builtPlatforms: new Set(),
  shippingLoaded: false,
  guideRendered: false,
  lastLogs: "",
  lastJournal: "",
  refreshing: false,
};

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const sec = (ns) => Math.round((Number(ns) || 0) / 1e9);
const groupId = (name) => "guide-group-" + String(name).toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

/* ── API y avisos ──────────────────────────────────────────────────────── */

async function call(method, path, body) {
  const res = await fetch(API + path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  // Sesión vencida o sin clave (A6): de vuelta a /acceso, que regresa a la consola.
  if (res.status === 401) {
    location.assign("/acceso?" + new URLSearchParams({ next: location.pathname + location.search }));
    throw new Error("sesión vencida: vuelve a ingresar la clave");
  }
  const data = res.status === 204 ? null : await res.json().catch(() => null);
  if (!res.ok) throw new Error((data && data.error) || ("HTTP " + res.status));
  return data;
}

function toast(message, kind = "ok") {
  const box = $("#toasts");
  const el = document.createElement("div");
  el.className = "toast " + kind;
  const text = document.createElement("span");
  text.textContent = message;
  el.append(text);
  if (kind === "bad") {
    const close = document.createElement("button");
    close.type = "button";
    close.className = "toast-close";
    close.innerHTML = icon("x", "", 2.2);
    close.setAttribute("aria-label", "Cerrar aviso");
    close.addEventListener("click", () => el.remove());
    el.append(close);
  } else {
    setTimeout(() => el.remove(), 4000);
  }
  box.append(el);
}

async function act(fn, okMessage) {
  try {
    const out = await fn();
    if (okMessage) toast(okMessage);
    return out;
  } catch (e) {
    toast("Error: " + e.message, "bad");
    return null;
  } finally {
    refresh();
  }
}

/* ── Estado y pintura ──────────────────────────────────────────────────── */

function setConn(kind, text, title) {
  $("#conn-dot").className = "dot " + kind;
  $("#conn-text").textContent = text;
  $("#conn-pill").title = title || text;
}

function paintConn() {
  if (!state.statusOk || !state.status) {
    setConn("caido", "sin conexión con demo-lab");
    $("#profile").textContent = "sin conexión con demo-lab";
    return;
  }
  const st = state.status;
  const b = st.bootstrap || {};
  $("#profile").textContent = `perfil ${st.profile} → ${st.sentinel_url}`;
  const error = b.last_error ? " · " + b.last_error : "";
  switch (b.state) {
    case "listo":
      setConn("ok", `Sentinel conectado · ${st.profile}`, `${st.sentinel_url}${error}`);
      break;
    case "conectando":
      setConn("degradado", "Conectando con Sentinel…", `${st.sentinel_url}${error}`);
      break;
    case "sin_credenciales":
      setConn("caido", "Sin credenciales de Sentinel", error || "Faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD");
      break;
    default:
      setConn("idle", "Iniciando…", st.sentinel_url || "");
  }
}

function activeAttacks(platform) {
  return state.attacks.filter((a) => !a.done && (!platform || a.platform === platform));
}

function faultName(f) {
  const p = f.params || {};
  switch (f.type) {
    case "down":
      return p.mode === "refused" ? "Cerrar puerto" : "Responder 503";
    case "degraded":
      return `Lentitud ${p.latency_ms || 950} ms`;
    case "errors":
      return `Errores ${Math.round((p.error_rate || 0.3) * 100)} %`;
    case "saturation":
      return "Saturar";
    case "slow_dependency":
      return `Dependencia lenta${p.target ? " · " + p.target : ""}`;
    default:
      return f.type;
  }
}

function kindLabel(name) {
  const kind = state.kinds.find((k) => k.name === name);
  return kind ? kind.label : name;
}

function actionLabel(platform, action) {
  return (ACTION_LABELS[platform] && ACTION_LABELS[platform][action]) || action;
}

function paintTabs() {
  for (const p of state.platforms) {
    const dot = $(`[data-tab-dot="${p.platform}"]`);
    if (dot) dot.className = "dot " + (p.state || "idle");
    const count = $(`[data-tab-count="${p.platform}"]`);
    if (!count) continue;
    const faults = (p.services || []).reduce((n, s) => n + (s.faults || []).length, 0);
    const n = faults + activeAttacks(p.platform).length;
    count.hidden = n === 0;
    count.textContent = String(n);
  }
}

function setCheck(name, kind, detail) {
  const li = $(`[data-check="${name}"]`);
  if (!li) return;
  const dot = $(".dot", li);
  dot.className = "dot " + kind;
  $("[data-check-detail]", li).textContent = detail;
}

function paintChecklist() {
  if (!state.statusOk || !state.status) {
    setCheck("sentinel", "idle", "Sin conexión con demo-lab.");
    setCheck("scenario", "idle", "Sin comprobar.");
    setCheck("nodes", "idle", "Sin comprobar.");
    setCheck("traffic", "idle", "Sin datos.");
    setCheck("warmup", "idle", "Empieza cuando haya tráfico.");
    return;
  }

  const st = state.status;
  const b = st.bootstrap || {};
  if (b.state === "listo") {
    setCheck("sentinel", "ok", `Conectado a ${st.sentinel_url}.`);
  } else if (b.state === "conectando") {
    setCheck("sentinel", "degradado", "Sentinel no responde; reintentando. Revisa que esté levantado.");
  } else if (b.state === "sin_credenciales") {
    setCheck("sentinel", "caido", b.last_error || "Faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD.");
  } else {
    setCheck("sentinel", "degradado", "Iniciando la conexión.");
  }

  // El error técnico queda al pasar el cursor, no en la lista.
  $('[data-check="sentinel"]').title = b.state === "conectando" ? (b.last_error || "") : "";

  if (state.verifying) {
    setCheck("scenario", "idle", "Comprobando…");
  } else if (!state.verify) {
    setCheck("scenario", "idle", "Sin comprobar: pulsa «Verificar conexión».");
  } else {
    const required = ["reachable", "login", "projects", "alerts", "ingest", "probe", "portal"];
    const bad = required.filter((k) => !(state.verify[k] && state.verify[k].ok));
    const channels = state.verify.notifications;
    const channelNote = !channels
      ? ""
      : channels.ok
        ? " Canales fuera del correo activos."
        : ` Canales: ${channels.detail || "solo correo"} (opcional).`;
    if (bad.length) {
      const detail = bad.map((k) => {
        const c = state.verify[k] || {};
        return `${CHECK_LABELS[k] || k}: ${c.detail || "falla"}`;
      }).join("; ");
      setCheck("scenario", "caido", detail + channelNote);
    } else {
      setCheck("scenario", "ok", "Login, proyectos, alertas, ingesta, sondeo y portal en verde." + channelNote);
    }
  }

  const faults = state.platforms.reduce((n, p) => n + (p.services || []).reduce((m, s) => m + (s.faults || []).length, 0), 0);
  const attacks = activeAttacks().length;
  if (!state.verify) {
    setCheck("nodes", "idle", "Sin comprobar: pulsa «Verificar conexión».");
  } else if (state.verifying) {
    setCheck("nodes", "idle", "Comprobando…");
  } else if (state.verify.nodes && state.verify.nodes.ok) {
    setCheck("nodes", "ok", "Los 16 servicios del mapa tienen sondeo propio.");
  } else {
    const nodes = state.verify.nodes || {};
    setCheck("nodes", "caido", nodes.detail || "Hay nodos del mapa sin sondeo.");
  }


  const bySlug = Object.fromEntries(state.platforms.map((p) => [p.platform, Number(p.traffic_rps || 0)]));
  const rps = PLATFORMS.map((slug) => bySlug[slug] ?? 0);
  const hasData = state.platforms.length === PLATFORMS.length;
  const allTraffic = hasData && rps.every((v) => v > 0);
  if (!state.platforms.length) {
    setCheck("traffic", "idle", "Sin datos de tráfico.");
  } else if (allTraffic) {
    setCheck("traffic", "ok", `Mercado ${rps[0]}/s · Banco ${rps[1]}/s · Clínica ${rps[2]}/s.`);
  } else {
    const off = PLATFORMS.filter((slug) => !(bySlug[slug] > 0)).map((slug) => PLATFORM_SHORT[slug]);
    setCheck("traffic", "degradado", `En 0: ${off.join(", ")}.`);
  }

  if (!allTraffic) {
    state.warmupSince = null;
    setCheck("warmup", "idle", "Empieza cuando haya tráfico en las tres.");
  } else {
    if (!state.warmupSince) state.warmupSince = Date.now();
    const minutes = (Date.now() - state.warmupSince) / 60000;
    if (minutes >= 10) {
      setCheck("warmup", "ok", "10 minutos cumplidos. La ventana de sondeos está caliente.");
    } else {
      setCheck("warmup", "degradado", `${minutes.toFixed(1)} de 10 min · medido desde que se abrió esta consola.`);
    }
  }
}

function paintVerifyNote() {
  const note = $("#verify-note");
  if (state.verifying) {
    note.textContent = "Verificando conexión…";
  } else if (state.verifyAt) {
    const t = new Date(state.verifyAt).toLocaleTimeString("es-CL", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    note.textContent = `Última verificación: ${t}.`;
  } else {
    note.textContent = "La verificación se ejecuta sola al abrir Inicio.";
  }
}


function activeKey(item) {
  return item.kind === "fault" ? "fault:" + item.fault.id : "attack:" + item.attack.id;
}

function createActiveRow() {
  const row = document.createElement("div");
  row.className = "active-row";
  const dot = document.createElement("span");
  dot.className = "dot";
  dot.setAttribute("aria-hidden", "true");
  const text = document.createElement("span");
  text.className = "active-text";
  text.style.flex = "1 1 280px";
  const action = document.createElement("button");
  action.type = "button";
  row.append(dot, text, action);
  return row;
}

function updateActiveRow(row, item) {
  const dot = $(".dot", row);
  const text = $(".active-text", row);
  const action = $("button", row);
  if (item.kind === "fault") {
    dot.className = "dot caido";
    text.textContent = `${PLATFORM_SHORT[item.platform]} · ${item.service} · ${faultName(item.fault)}`;
    action.innerHTML = `${icon("x", "", 2.2)} Retirar`;
    action.dataset.removeFault = item.fault.id;
    action.dataset.guide = "fault-remove";
    delete action.dataset.stopAttack;
  } else {
    dot.className = "dot degradado";
    text.textContent = `${PLATFORM_SHORT[item.attack.platform]} · ${kindLabel(item.attack.kind)} contra ${item.attack.target} · ${item.attack.sent} eventos`;
    action.textContent = "Detener";
    action.dataset.stopAttack = item.attack.id;
    action.dataset.guide = "attack-stop";
    delete action.dataset.removeFault;
  }
}


function paintHomeActive() {
  const box = $("#home-active");
  const rows = [];
  for (const p of state.platforms) {
    for (const s of p.services || []) {
      for (const f of s.faults || []) rows.push({ kind: "fault", platform: p.platform, service: s.slug, fault: f });
    }
  }
  for (const a of activeAttacks()) rows.push({ kind: "attack", attack: a });
  box.className = "active-list";
  syncKeyed(box, rows, activeKey, createActiveRow, updateActiveRow, "Sin fallas ni ataques activos.");
}

function maybeAutoVerify() {
  if (state.tab !== "inicio" || state.verifying || !state.statusOk) return;
  if (!state.verify || Date.now() - state.verifyAt > 120000) runVerify(true);
}

function paintHome() {
  if (!state.statusOk || !state.status) {
    paintChecklist();
    paintVerifyNote();
    return;
  }
  paintChecklist();
  paintHomeActive();
  paintVerifyNote();
  maybeAutoVerify();
}

function paintShipping() {
  const sh = state.status && state.status.shipping;
  if (!sh) return;
  const p = sh.policy || {};
  if (!state.shippingLoaded) {
    $("#sh-enabled").checked = !!p.enabled;
    $("#sh-level").value = p.level_min;
    $("#sh-sample").value = p.info_sample_rate;
    $("#sh-budget").value = p.budget_per_min;
    state.shippingLoaded = true;
  }
  const m = sh.metrics || {};
  const stats = [
    ["Enviados", m.sent], ["Aceptados", m.accepted_processed], ["Incompletos", m.accepted_incomplete],
    ["Descartados por Sentinel", m.discarded_by_sentinel], ["Pendientes", m.pending],
    ["Filtrados", m.filtered_by_policy], ["Sobre presupuesto", m.over_budget], ["Dead-letter", m.dead_letter],
  ];
  $("#metrics").innerHTML = stats.map(([k, v]) => `<div><dt>${k}</dt><dd>${Number(v || 0)}</dd></div>`).join("");
  const err = $("#metrics-error");
  err.hidden = !m.last_error;
  err.textContent = m.last_error ? "Último error: " + m.last_error : "";
}

function paint() {
  paintConn();
  paintTabs();
  paintHome();
  for (const p of state.platforms) paintPlatform(p);
  paintShipping();
}

/* ── Pestaña de plataforma ─────────────────────────────────────────────── */

function ensurePlatformPanels() {
  for (const p of state.platforms) {
    if (state.builtPlatforms.has(p.platform)) continue;
    buildPlatformPanel(p);
    state.builtPlatforms.add(p.platform);
  }
}

function buildPlatformPanel(p) {
  const host = $(`[data-platform-host="${p.platform}"]`);
  if (!host) return;
  host.innerHTML = `
    <header class="page-head platform-top">
      <div class="page-title">
        <h2><span data-platform-name></span> <span class="pill" data-platform-state></span></h2>
        <p class="muted small" data-platform-sub></p>
        <p class="muted small" data-platform-portal hidden></p>
      </div>
      <div class="row platform-actions">
        <label class="traffic">Tráfico
          <input type="number" min="0" max="50" step="0.5" inputmode="decimal" data-traffic="${esc(p.platform)}" data-guide="general-traffic" aria-label="Usuarios por segundo de ${esc(p.name)}">
          <span>usuarios/s</span>
          <button type="button" class="help" data-guide="general-traffic" aria-label="Explicar el tráfico">?</button>
        </label>
        <span class="sep" aria-hidden="true"></span>
        <button type="button" class="good" data-restore="${esc(p.platform)}" data-guide="general-restore">Restaurar plataforma</button>
        <a class="btn site" href="/sitios/${esc(p.platform)}/" target="_blank" rel="noopener" data-guide="general-open-site">Abrir sitio ↗</a>
      </div>
    </header>

    <div class="layout">
      <div class="layout-main">
        <section class="card">
          <h2>Servicios <button type="button" class="help" data-guide="group-services" aria-label="Explicar los servicios">?</button> <span class="muted small">clic en uno para provocar una falla</span>
            <button type="button" class="link h2-link" data-log-platform="${esc(p.platform)}" data-guide="platform-logs-link">Ver logs →</button></h2>
          <div class="services" data-services></div>
        </section>

        <section class="card">
          <h2>Probar como usuario <button type="button" class="help" data-guide="user-actions" aria-label="Explicar las acciones de usuario">?</button></h2>
          <div class="row" data-user-actions></div>
        </section>
      </div>

      <aside class="layout-side">
        <section class="card">
          <h2>Guiones <button type="button" class="help" data-guide="group-scripts" aria-label="Explicar los guiones">?</button></h2>
          <div class="script-list" data-scripts></div>
          <h3 class="subhead">Ejecuciones <button type="button" class="help" data-guide="script-run-control" aria-label="Explicar el control de ejecuciones">?</button></h3>
          <div class="run-list" data-runs></div>
        </section>

        <section class="card">
          <h2>Ataque <button type="button" class="help" data-guide="group-attacks" aria-label="Explicar los ataques">?</button></h2>
          <div class="attack-form">
            <label class="span2">Tipo <select data-attack-kind data-guide="attack-launch"></select><button type="button" class="help" data-attack-guide aria-label="Explicar el tipo de ataque">?</button></label>
            <label class="span2">Objetivo <select data-attack-target data-guide="attack-launch"></select></label>
            <label>Eventos/s <input type="number" min="1" max="10" value="2" data-attack-intensity data-guide="attack-launch"></label>
            <label>Duración (s) <input type="number" min="1" max="600" value="40" data-attack-duration data-guide="attack-launch"></label>
            <button type="button" class="danger span2" data-attack-launch data-guide="attack-launch">Lanzar ataque</button>
          </div>
          <div class="attack-list" data-attacks></div>
        </section>
      </aside>
    </div>`;

  const services = $("[data-services]", host);
  for (const s of p.services || []) services.append(buildService(p, s));

  const actions = $("[data-user-actions]", host);
  for (const name of p.actions) {
    const b = document.createElement("button");
    b.type = "button";
    b.dataset.user = `${p.platform}/${name}`;
    b.dataset.guide = "user-actions";
    const label = actionLabel(p.platform, name);
    b.textContent = label[0].toUpperCase() + label.slice(1);
    actions.append(b);
  }
}

function buildService(p, s) {
  const row = document.createElement("div");
  row.className = "service";
  row.dataset.service = `${p.platform}/${s.slug}`;

  const head = document.createElement("div");
  head.className = "service-head";
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "service-toggle";
  toggle.setAttribute("aria-expanded", "false");
  toggle.dataset.guide = "group-services";
  toggle.innerHTML = `<span class="dot idle" aria-hidden="true"></span><b>${esc(s.slug)}</b><span class="role muted">${esc(s.role)}</span><span class="service-metrics"></span><span class="chev" aria-hidden="true">${icon("chevron-right", "", 2)}</span>`;

  const active = document.createElement("div");
  active.className = "service-active";
  head.append(toggle, active);

  const detail = document.createElement("div");
  detail.className = "service-detail";
  detail.hidden = true;
  detail.innerHTML = `
    <div class="detail-head"><span class="muted small">Provocar falla</span><button type="button" class="help" data-guide="fault-remove" aria-label="Explicar cómo se retira una falla">?</button></div>
    <div class="fault-grid">${FAULTS.map((f) =>
      `<button type="button" class="fault-btn" data-fault="${esc(p.platform)}/${esc(s.slug)}/${f.guide}" data-guide="${f.guide}"><b>${esc(f.label)}</b><small>${esc((GUIDE[f.guide] && GUIDE[f.guide].what) || "")}</small></button>`
    ).join("")}</div>`;

  row.append(head, detail);
  return row;
}

function paintPlatform(p) {
  const host = $(`[data-platform-host="${p.platform}"]`);
  if (!host) return;

  $("[data-platform-name]", host).textContent = p.name;
  const pill = $("[data-platform-state]", host);
  pill.className = "pill " + (p.state || "");
  pill.textContent = STATE_LABEL[p.state] || p.state || "";

  const services = p.services || [];
  const faults = services.reduce((n, s) => n + (s.faults || []).length, 0);
  const attacks = activeAttacks(p.platform).length;
  $("[data-platform-sub]", host).textContent =
    `${services.length} servicios · ${faults} falla(s) activas · ${attacks} ataque(s) activos.`;
  const portal = $("[data-platform-portal]", host);
  portal.hidden = !p.portal_email;
  const portalHtml = p.portal_email
    ? `Portal del cliente: <b>${esc(p.portal_email)}</b>${p.portal_url ? ` · <a href="${esc(p.portal_url)}" target="_blank" rel="noopener">Abrir portal ↗</a>` : ""} <button type="button" class="help" data-guide="general-portal" aria-label="Explicar el portal del cliente">?</button>`
    : "";
  if (portal.innerHTML !== portalHtml) portal.innerHTML = portalHtml;

  const traffic = $(`[data-traffic="${p.platform}"]`, host);
  if (traffic && document.activeElement !== traffic) traffic.value = String(Number(p.traffic_rps || 0));

  for (const s of services) {
    const row = $(`[data-service="${p.platform}/${s.slug}"]`, host);
    if (!row) continue;
    const toggle = $(".service-toggle", row);
    $(".dot", toggle).className = "dot " + (s.state || "idle");
    const metrics = $(".service-metrics", toggle);
    metrics.textContent = `${s.requests_last_min} pet · ${s.errors_last_min} err.`;
    metrics.className = "service-metrics" + (s.errors_last_min ? " caido" : "");
    syncFaults($(".service-active", row), s.faults || []);
  }

  paintScripts(p, host);
  paintAttacks(p, host);
}

function syncFaults(container, faults) {
  const seen = new Set();
  for (const f of faults) {
    seen.add(f.id);
    let chip = container.querySelector(`[data-fault-chip="${f.id}"]`);
    if (!chip) {
      chip = document.createElement("span");
      chip.className = "chip";
      chip.dataset.faultChip = f.id;
      const label = document.createElement("span");
      label.className = "chip-label";
      const x = document.createElement("button");
      x.type = "button";
      x.className = "chip-x";
      x.dataset.removeFault = f.id;
      x.innerHTML = icon("x", "", 2.2);
      x.title = "Retirar falla";
      chip.append(label, x);
      container.append(chip);
    }
    const name = faultName(f);
    $(".chip-label", chip).textContent = name;
    const x = $(".chip-x", chip);
    x.setAttribute("aria-label", `Retirar ${name} de ${f.service}`);
    x.title = `Retirar ${name}`;
  }
  for (const chip of $$("[data-fault-chip]", container)) {
    if (!seen.has(chip.dataset.faultChip)) chip.remove();
  }
}

function syncKeyed(container, items, keyFn, createFn, updateFn, emptyText) {
  if (!items.length) {
    if (container.dataset.empty !== emptyText) {
      container.replaceChildren();
      const msg = document.createElement("span");
      msg.className = "muted small";
      msg.textContent = emptyText;
      container.append(msg);
      container.dataset.empty = emptyText;
    }
    return;
  }
  delete container.dataset.empty;
  for (const el of [...container.children]) if (!el.dataset.key) el.remove();
  const existing = new Map();
  for (const el of [...container.children]) {
    if (el.dataset.key) existing.set(el.dataset.key, el);
  }
  const seen = new Set();
  for (const item of items) {
    const key = keyFn(item);
    seen.add(key);
    let el = existing.get(key);
    if (!el) {
      el = createFn(item);
      el.dataset.key = key;
      container.append(el);
    }
    updateFn(el, item);
  }
  for (const [key, el] of existing) if (!seen.has(key)) el.remove();
}

function createScriptRow(s) {
  const row = document.createElement("div");
  row.className = "script-row";
  const title = document.createElement("div");
  title.className = "script-title";
  const b = document.createElement("b");
  b.textContent = s.titulo;
  const meta = document.createElement("span");
  meta.className = "muted small";
  meta.textContent = `${s.duration_s} s · ${s.pasos.length} pasos`;
  title.append(b, document.createElement("br"), meta);

  const actions = document.createElement("div");
  actions.className = "row";
  const help = document.createElement("button");
  help.type = "button";
  help.className = "help";
  help.dataset.guide = "script-" + s.id;
  help.textContent = "?";
  help.setAttribute("aria-label", `Explicar el guion ${s.titulo}`);
  const run = document.createElement("button");
  run.type = "button";
  run.className = "good";
  run.dataset.runScript = s.id;
  run.dataset.guide = "script-" + s.id;
  run.textContent = "Ejecutar";
  actions.append(help, run);
  row.append(title, actions);
  return row;
}

function paintScripts(p, host) {
  const active = state.runs.some((r) => r.platform === p.platform && (r.state === "running" || r.state === "paused"));
  const scripts = state.scripts.filter((s) => s.plataforma === p.platform);
  syncKeyed(
    $("[data-scripts]", host),
    scripts,
    (s) => s.id,
    createScriptRow,
    (el, s) => {
      const run = $("[data-run-script]", el);
      run.disabled = active;
      run.title = active ? "Ya hay una ejecución activa en esta plataforma" : "";
      $(".script-title .muted", el).textContent = `${s.duration_s} s · ${s.pasos.length} pasos`;
    },
    "Sin guiones para esta plataforma."
  );

  const runs = state.runs.filter((r) => r.platform === p.platform).slice(0, 5);
  syncKeyed(
    $("[data-runs]", host),
    runs,
    (r) => r.id,
    createRunRow,
    updateRunRow,
    "Sin ejecuciones."
  );
}

function createRunRow() {
  const row = document.createElement("div");
  row.className = "run-row";
  const main = document.createElement("div");
  main.className = "run-main";
  const stateIcon = document.createElement("span");
  stateIcon.className = "run-state";
  const title = document.createElement("b");
  title.className = "run-title";
  const meta = document.createElement("span");
  meta.className = "muted small run-meta";
  main.append(stateIcon, title, document.createTextNode(" "), meta);
  const actions = document.createElement("div");
  actions.className = "row run-actions";
  row.append(main, actions);
  return row;
}

function updateRunRow(el, r) {
  const icons = { running: "play", paused: "pause", finished: "circle-check", aborted: "circle-x" };
  $(".run-state", el).innerHTML = icons[r.state] ? icon(icons[r.state], "", 2) : "…";
  $(".run-title", el).textContent = r.title;
  $(".run-meta", el).textContent = `${sec(r.elapsed_ns)}/${sec(r.total_ns)} s · paso ${Math.min(r.next_step + 1, r.steps)}/${r.steps}`;
  const actions = $(".run-actions", el);
  actions.replaceChildren();
  if (r.state === "running") actions.append(actionButton("Pausar", "", "runOp", `${r.id}/pause`));
  if (r.state === "paused") actions.append(actionButton("Reanudar", "", "runOp", `${r.id}/resume`));
  if (r.state === "running" || r.state === "paused") {
    actions.append(actionButton("Abortar", "danger", "runOp", `${r.id}/abort`));
  }
}

function actionButton(label, className, datasetKey, value) {
  const b = document.createElement("button");
  b.type = "button";
  if (className) b.className = className;
  b.dataset[datasetKey] = value;
  b.dataset.guide = "script-run-control";
  b.textContent = label;
  return b;
}

function paintAttacks(p, host) {
  const platformServices = new Set((p.services || []).map((s) => s.slug));
  const kinds = state.kinds.filter((k) => k.targets.some((t) => platformServices.has(t)));
  const kindSel = $("[data-attack-kind]", host);
  const signature = kinds.map((k) => k.name).join("|");
  if (kindSel.dataset.signature !== signature) {
    const previous = kindSel.value;
    kindSel.replaceChildren(...kinds.map((k) => new Option(k.label, k.name)));
    kindSel.dataset.signature = signature;
    if (kinds.some((k) => k.name === previous)) kindSel.value = previous;
  }
  const kind = kinds.find((k) => k.name === kindSel.value) || kinds[0];
  if (kind && kindSel.value !== kind.name) kindSel.value = kind.name;

  const help = $("[data-attack-guide]", host);
  if (kind) {
    help.dataset.guide = "attack-" + kind.name;
    help.hidden = false;
  } else {
    help.hidden = true;
    delete help.dataset.guide;
  }
  fillAttackTargets(host);

  const attacks = state.attacks.filter((a) => a.platform === p.platform).slice(0, 6);
  syncKeyed(
    $("[data-attacks]", host),
    attacks,
    (a) => a.id,
    createAttackRow,
    updateAttackRow,
    "Sin ataques."
  );
}

function fillAttackTargets(host) {
  const platform = host.dataset.platformHost;
  const p = state.platforms.find((x) => x.platform === platform);
  if (!p) return;
  const kindSel = $("[data-attack-kind]", host);
  const targetSel = $("[data-attack-target]", host);
  const kind = state.kinds.find((k) => k.name === kindSel.value);
  const slugs = new Set((p.services || []).map((s) => s.slug));
  const targets = kind ? kind.targets.filter((t) => slugs.has(t)) : [];
  const signature = targets.join("|");
  if (targetSel.dataset.signature !== signature) {
    const previous = targetSel.value;
    targetSel.replaceChildren(...targets.map((t) => new Option(t, t)));
    targetSel.dataset.signature = signature;
    if (targets.includes(previous)) targetSel.value = previous;
  }
}

function createAttackRow() {
  const row = document.createElement("div");
  row.className = "attack-row";
  const dot = document.createElement("span");
  dot.className = "dot";
  dot.setAttribute("aria-hidden", "true");
  const text = document.createElement("span");
  text.className = "attack-text";
  text.style.flex = "1 1 280px";
  const actions = document.createElement("div");
  actions.className = "row";
  row.append(dot, text, actions);
  return row;
}

function updateAttackRow(el, a) {
  $(".dot", el).className = "dot " + (a.done ? "idle" : "caido");
  $(".attack-text", el).textContent =
    `${a.done ? "terminado" : "en curso"} · ${kindLabel(a.kind)} → ${a.target} · ${a.sent} eventos`;
  const actions = $(".row", el);
  actions.replaceChildren();
  if (!a.done) {
    const stop = document.createElement("button");
    stop.type = "button";
    stop.textContent = "Detener";
    stop.dataset.stopAttack = a.id;
    stop.dataset.guide = "attack-stop";
    actions.append(stop);
  }
}

/* ── Actividad ─────────────────────────────────────────────────────────── */

async function refreshActivity() {
  if (state.tab !== "actividad") return;
  const query = new URLSearchParams({
    platform: $("#lg-platform").value,
    level: $("#lg-level").value,
  });
  const [logs, journal] = await Promise.allSettled([
    call("GET", "/logs?" + query),
    call("GET", "/journal"),
  ]);
  if (logs.status === "fulfilled") paintLogs(logs.value || []);
  if (journal.status === "fulfilled") paintJournal(journal.value || []);
}

function paintLogs(rows) {
  const signature = rows.map((r) => `${r.timestamp}|${r.severity}|${r.platform}|${r.service}|${r.message}`).join("\n");
  if (signature === state.lastLogs) return;
  state.lastLogs = signature;
  const pre = $("#logs");
  if (!rows.length) {
    pre.textContent = "Sin logs con ese filtro.";
    return;
  }
  const frag = document.createDocumentFragment();
  for (const r of rows.slice().reverse()) {
    const line = document.createElement("span");
    line.className = "log-line log-" + r.severity;
    line.textContent = `${String(r.timestamp).slice(11, 19)} ${String(r.severity).padEnd(8)} ${r.platform}/${r.service}  ${r.message}`;
    frag.append(line);
  }
  pre.replaceChildren(frag);
}

function paintJournal(entries) {
  const signature = entries.map((e) => `${e.time}|${e.kind}|${e.platform}|${e.summary}|${e.detail}`).join("\n");
  if (signature === state.lastJournal) return;
  state.lastJournal = signature;
  const pre = $("#journal");
  if (!entries.length) {
    pre.textContent = "Sin acciones registradas todavía.";
    return;
  }
  pre.textContent = entries.slice(-40).reverse()
    .map((e) => `${String(e.time).slice(11, 19)} ${String(e.kind).padEnd(10)} ${e.platform || ""} ${e.summary} ${e.detail || ""}`)
    .join("\n");
}

/* ── Maratón ───────────────────────────────────────────────────────────── */

async function refreshMarathon() {
  if (state.tab !== "datos") return;
  const el = $("#marathon");
  try {
    const m = await call("GET", "/marathon");
    const until = m.running && m.until ? " hasta " + String(m.until).slice(11, 16) + " UTC" : "";
    const lines = [m.running ? "En marcha" + until + "." : "Detenida."];
    for (const c of (m.cycles || []).slice(-5).reverse()) {
      lines.push(`${c.finished ? "listo" : "…"} ${c.scenario} · abrió ${(c.opened || []).length}, cerró ${(c.resolved || []).length}${c.error ? " · " + c.error : ""}`);
    }
    el.textContent = lines.join("\n");
  } catch {
    el.textContent = "Maratón no disponible.";
  }
}

/* ── Acciones ──────────────────────────────────────────────────────────── */

async function runVerify(silent) {
  if (state.verifying) return;
  state.verifying = true;
  const button = $("#btn-verify");
  const label = button.textContent;
  button.disabled = true;
  button.textContent = "Verificando…";
  paintHome();
  try {
    state.verify = await call("POST", "/connection/verify");
    state.verifyAt = Date.now();
    if (!silent) {
      const required = ["reachable", "login", "projects", "alerts", "ingest", "probe", "nodes", "portal"];
      const bad = required.filter((k) => !(state.verify[k] && state.verify[k].ok));
      toast(bad.length ? "Verificación con fallas: " + bad.map((k) => CHECK_LABELS[k] || k).join(", ") : "Conexión verificada", bad.length ? "bad" : "ok");
    }
  } catch (e) {
    state.verify = { reachable: { ok: false, detail: e.message } };
    state.verifyAt = Date.now();
    if (!silent) toast("No se pudo verificar: " + e.message, "bad");
  } finally {
    state.verifying = false;
    button.disabled = false;
    button.textContent = label;
    paintHome();
  }
}

async function doReset() {
  closeMenu();
  try {
    const r = await call("POST", "/reset");
    const detail = `Reiniciado: ${r.faults} fallas, ${r.attacks} ataques, ${r.runs} guiones, ${r.incidents} incidentes · tráfico base`;
    toast(detail + (r.incidents_error ? " · Sentinel: " + r.incidents_error : ""), r.incidents_error ? "bad" : "ok");
  } catch (e) {
    toast("Error: " + e.message, "bad");
  }
  refresh();
}

async function doRestoreAll() {
  closeMenu();
  await act(async () => {
    for (const p of PLATFORMS) await call("POST", `/platforms/${p}/restore`);
  }, "Todo restaurado");
}

async function doUserAction(platform, action) {
  const label = actionLabel(platform, action);
  try {
    const r = await call("POST", `/platforms/${platform}/actions/${action}`);
    if (r && r.ok === false) throw new Error(r.error || "la operación fue rechazada");
    toast(`«${label}» OK${r && r.receipt ? " · comprobante " + r.receipt : ""}`);
  } catch (e) {
    toast(`«${label}» FALLÓ · ${e.message}`, "bad");
  }
  refresh();
}

async function doLaunchAttack(host) {
  const platform = host.dataset.platformHost;
  const kind = $("[data-attack-kind]", host).value;
  const target = $("[data-attack-target]", host).value;
  const intensity = Number($("[data-attack-intensity]", host).value);
  const duration = Number($("[data-attack-duration]", host).value);
  if (!kind || !target) {
    toast("Elige un tipo de ataque y un objetivo.", "bad");
    return;
  }
  await act(() => call("POST", "/attacks", {
    platform,
    target,
    kind,
    intensity,
    duration_s: duration,
  }), "Ataque lanzado");
}

async function doSaveShipping() {
  await act(() => call("PUT", "/shipping", {
    enabled: $("#sh-enabled").checked,
    level_min: $("#sh-level").value,
    info_sample_rate: Number($("#sh-sample").value),
    budget_per_min: Number($("#sh-budget").value),
  }), "Política aplicada");
}

async function doReship() {
  try {
    const r = await call("POST", "/logs/ship", {
      level_min: "error",
      since: new Date(Date.now() - 600000).toISOString(),
    });
    toast(`Reenvío: ${r.accepted} aceptados, ${r.rejected} rechazados de ${r.queued}`);
  } catch (e) {
    toast("Error: " + e.message, "bad");
  }
}

async function doGenerateHistory() {
  try {
    const r = await call("POST", "/history", {
      days: Number($("#hs-days").value),
      incidents_per_day: Number($("#hs-per").value),
      mttr_profile: $("#hs-trend").value,
    });
    toast(`Historial: ${r.created} incidentes (${r.security} de seguridad), reemplazó ${r.cleared}`);
  } catch (e) {
    toast("Error: " + e.message, "bad");
  }
}

async function doClearHistory() {
  try {
    const r = await call("DELETE", "/history");
    toast(`Historial borrado: ${r.deleted}`);
  } catch (e) {
    toast("Error: " + e.message, "bad");
  }
}

/* ── Menú y confirmaciones ─────────────────────────────────────────────── */

function toggleMenu() {
  const menu = $("#menu");
  const open = menu.hidden;
  menu.hidden = !open;
  $("#btn-menu").setAttribute("aria-expanded", String(open));
  if (open) $("button", menu).focus();
}

function closeMenu() {
  const menu = $("#menu");
  if (menu.hidden) return;
  menu.hidden = true;
  $("#btn-menu").setAttribute("aria-expanded", "false");
}

const confirmTimers = new WeakMap();

function confirmAction(button, message, fn) {
  const armed = confirmTimers.get(button);
  if (armed) {
    clearTimeout(armed.timer);
    confirmTimers.delete(button);
    button.textContent = armed.label;
    button.classList.remove("confirming");
    fn();
    return;
  }
  const label = button.textContent;
  const timer = setTimeout(() => {
    button.textContent = label;
    button.classList.remove("confirming");
    confirmTimers.delete(button);
  }, 4000);
  confirmTimers.set(button, { label, timer });
  button.textContent = message;
  button.classList.add("confirming");
}

/* ── Pestañas ──────────────────────────────────────────────────────────── */

function setTab(name) {
  if (!TABS.has(name)) name = "inicio";
  state.tab = name;
  for (const button of $$(".tabs [role=tab]")) {
    const active = button.dataset.tab === name;
    button.setAttribute("aria-selected", String(active));
    button.tabIndex = active ? 0 : -1;
  }
  for (const panel of $$(".panel")) panel.hidden = panel.id !== "panel-" + name;
  if (location.hash !== "#" + name) history.replaceState(null, "", "#" + name);
  closeMenu();
  if (name === "actividad") refreshActivity();
  if (name === "datos") refreshMarathon();
  if (name === "inicio") maybeAutoVerify();
}

/* ── Guía ──────────────────────────────────────────────────────────────── */

function renderGuide() {
  if (state.guideRendered) return;
  const body = $("#guide-body");
  const op = document.createElement("section");
  op.className = "guide-op";
  op.id = "guide-operacion";
  const title = document.createElement("h3");
  title.textContent = "Cómo operar";
  op.append(title);
  for (const block of OPERATION) {
    const h = document.createElement("h4");
    h.textContent = block.title;
    const ul = document.createElement("ul");
    for (const item of block.items) {
      const li = document.createElement("li");
      li.textContent = item;
      ul.append(li);
    }
    op.append(h, ul);
  }
  body.append(op);

  for (const group of GROUPS) {
    const entries = Object.entries(GUIDE).filter(([, entry]) => entry.group === group);
    if (!entries.length) continue;
    const section = document.createElement("section");
    section.className = "guide-group";
    section.id = groupId(group);
    const h = document.createElement("h3");
    h.className = "group-title";
    h.textContent = group;
    section.append(h);
    for (const [id, entry] of entries) {
      const details = document.createElement("details");
      details.id = "guide-" + id;
      details.dataset.guideEntry = id;
      const summary = document.createElement("summary");
      summary.textContent = entry.label;
      const dl = document.createElement("dl");
      for (const [label, value] of [["Qué hace", entry.what], ["En el sitio", entry.site], ["En Sentinel", entry.sentinel], ["Cómo se deshace", entry.undo]]) {
        const dt = document.createElement("dt");
        dt.textContent = label;
        const dd = document.createElement("dd");
        dd.textContent = value;
        dl.append(dt, dd);
      }
      details.append(summary, dl);
      section.append(details);
    }
    body.append(section);
  }
  state.guideRendered = true;
}

function openGuide(target) {
  renderGuide();
  const aside = $("#guide");
  aside.hidden = false;
  $("#btn-guide").setAttribute("aria-expanded", "true");

  let el = target ? document.getElementById("guide-" + target) : null;
  if (!el) {
    const group = GROUPS.includes(target) ? target : (TAB_GROUP[state.tab] || "Generales");
    el = document.getElementById(groupId(group));
  }
  if (el) {
    if (el.tagName === "DETAILS") el.open = true;
    el.scrollIntoView({ block: "start" });
  }
  $("#guide-close").focus({ preventScroll: true });
}

function closeGuide() {
  $("#guide").hidden = true;
  $("#btn-guide").setAttribute("aria-expanded", "false");
  $("#btn-guide").focus({ preventScroll: true });
}

/* ── Sondeos ───────────────────────────────────────────────────────────── */

async function refresh() {
  if (state.refreshing) return;
  state.refreshing = true;
  try {
    const [status, platforms, attacks, scripts] = await Promise.allSettled([
      call("GET", "/status"),
      call("GET", "/platforms"),
      call("GET", "/attacks"),
      call("GET", "/scripts"),
    ]);
    state.statusOk = status.status === "fulfilled" && !!status.value;
    if (state.statusOk) state.status = status.value;
    if (platforms.status === "fulfilled" && Array.isArray(platforms.value)) state.platforms = platforms.value;
    if (attacks.status === "fulfilled" && attacks.value) {
      state.kinds = attacks.value.kinds || [];
      state.attacks = attacks.value.attacks || [];
    }
    if (scripts.status === "fulfilled" && scripts.value) {
      state.scripts = scripts.value.scripts || [];
      state.runs = scripts.value.runs || [];
    }
    if (state.platforms.length) ensurePlatformPanels();
    paint();
  } finally {
    state.refreshing = false;
  }
}

function fillLogPlatforms() {
  const select = $("#lg-platform");
  for (const slug of PLATFORMS) {
    if (!select.querySelector(`option[value="${slug}"]`)) select.add(new Option(PLATFORM_SHORT[slug], slug));
  }
}

function setupSitesHint() {
  const hint = $("#sites-hint");
  if (!hint) return;
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname);
  hint.textContent = `Dirección de los sitios: ${location.origin}/sitios/` +
    (local ? " · desde otro equipo o tablet, cambia localhost por la IP de esta máquina." : "");
}

/* ── Eventos ───────────────────────────────────────────────────────────── */

document.addEventListener("click", (e) => {
  const help = e.target.closest(".help[data-guide]");
  if (help) {
    e.preventDefault();
    openGuide(help.dataset.guide);
    return;
  }

  const target = e.target.closest("button, a");
  if (!target) {
    closeMenu();
    return;
  }
  if (!target.closest(".menu-wrap")) closeMenu();
  const d = target.dataset;

  if (target.id === "conn-pill") {
    setTab("inicio");
    return;
  }
  if (target.id === "btn-menu") {
    e.preventDefault();
    toggleMenu();
    return;
  }
  if (target.id === "btn-theme") {
    setTheme(document.documentElement.dataset.theme === "light" ? "dark" : "light");
    return;
  }
  if (target.id === "btn-guide") {
    openGuide(TAB_GROUP[state.tab]);
    return;
  }
  if (target.id === "guide-close") {
    closeGuide();
    return;
  }
  if (target.id === "btn-verify") {
    runVerify(false);
    return;
  }
  if (target.id === "btn-provision") {
    act(() => call("POST", "/provision"), "Escenario preparado en Sentinel");
    return;
  }
  if (target.id === "btn-restore-all") {
    doRestoreAll();
    return;
  }
  if (target.id === "btn-reset") {
    confirmAction(target, "¿Seguro? Cierra los incidentes abiertos", doReset);
    return;
  }
  if (target.id === "btn-shipping") {
    doSaveShipping();
    return;
  }
  if (target.id === "btn-reship") {
    doReship();
    return;
  }
  if (target.id === "btn-history") {
    doGenerateHistory();
    return;
  }
  if (target.id === "btn-history-del") {
    confirmAction(target, "¿Seguro? Borra el historial", doClearHistory);
    return;
  }
  if (target.id === "btn-marathon") {
    act(() => call("POST", "/marathon", {
      hours: Number($("#mr-hours").value),
      cadence_min: Number($("#mr-cad").value),
    }), "Maratón iniciada").then(refreshMarathon);
    return;
  }
  if (target.id === "btn-marathon-stop") {
    act(() => call("DELETE", "/marathon"), "Maratón detenida").then(refreshMarathon);
    return;
  }
  if (target.id === "btn-logs-refresh") {
    state.lastLogs = "";
    refreshActivity();
    return;
  }
  if (d.tab) {
    setTab(d.tab);
    return;
  }
  if (d.goTab) {
    setTab(d.goTab);
    return;
  }
  if (target.classList.contains("service-toggle")) {
    toggleService(target);
    return;
  }
  if (d.fault) {
    const [platform, service, guide] = d.fault.split("/");
    const fault = FAULTS.find((f) => f.guide === guide);
    if (fault) act(() => call("POST", "/faults", {
      platform,
      service,
      type: fault.type,
      params: fault.params,
    }), `${fault.label} en ${service}`);
    return;
  }
  if (d.removeFault) {
    act(() => call("DELETE", "/faults/" + d.removeFault), "Falla retirada");
    return;
  }
  if (d.restore) {
    act(() => call("POST", `/platforms/${d.restore}/restore`), "Plataforma restaurada");
    return;
  }
  if (d.user) {
    const [platform, action] = d.user.split("/");
    doUserAction(platform, action);
    return;
  }
  if (d.logPlatform) {
    $("#lg-platform").value = d.logPlatform;
    state.lastLogs = "";
    setTab("actividad");
    return;
  }
  if (d.runScript) {
    act(() => call("POST", `/scripts/${d.runScript}/run`), "Guion iniciado");
    return;
  }
  if (d.runOp) {
    const [id, op] = d.runOp.split("/");
    const messages = { pause: "Guion pausado", resume: "Guion reanudado", abort: "Guion abortado" };
    act(() => call("POST", `/runs/${id}/${op}`), messages[op] || "Guion actualizado");
    return;
  }
  if (d.stopAttack) {
    act(() => call("DELETE", "/attacks/" + d.stopAttack), "Ataque detenido");
    return;
  }
  if (d.attackLaunch !== undefined) {
    const host = target.closest("[data-platform-host]");
    if (host) doLaunchAttack(host);
  }
});

document.addEventListener("change", (e) => {
  const target = e.target;
  if (target.dataset && target.dataset.traffic) {
    act(() => call("PUT", "/traffic/" + target.dataset.traffic, { rps: Number(target.value) }), "Tráfico actualizado");
    return;
  }
  if (target.matches("[data-attack-kind]")) {
    const host = target.closest("[data-platform-host]");
    if (host) fillAttackTargets(host);
    return;
  }
  if (target.id === "lg-platform" || target.id === "lg-level") {
    state.lastLogs = "";
    refreshActivity();
  }
});

function toggleService(button) {
  const row = button.closest(".service");
  const detail = $(".service-detail", row);
  const wasOpen = !detail.hidden;
  for (const other of $$(".service-detail")) {
    other.hidden = true;
    const toggle = other.closest(".service").querySelector(".service-toggle");
    toggle.setAttribute("aria-expanded", "false");
    $(".chev", toggle).style.rotate = "0deg";
  }
  if (!wasOpen) {
    detail.hidden = false;
    button.setAttribute("aria-expanded", "true");
    $(".chev", button).style.rotate = "90deg";
  }
}

$(".tabs").addEventListener("keydown", (e) => {
  const tabs = $$("[role=tab]", e.currentTarget);
  const index = tabs.indexOf(document.activeElement);
  if (index < 0) return;
  let next;
  if (e.key === "ArrowRight") next = (index + 1) % tabs.length;
  else if (e.key === "ArrowLeft") next = (index - 1 + tabs.length) % tabs.length;
  else if (e.key === "Home") next = 0;
  else if (e.key === "End") next = tabs.length - 1;
  else return;
  e.preventDefault();
  tabs[next].focus();
  setTab(tabs[next].dataset.tab);
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (!$("#guide").hidden) closeGuide();
  else closeMenu();
});

window.addEventListener("hashchange", () => {
  const name = location.hash.replace("#", "");
  if (TABS.has(name) && name !== state.tab) setTab(name);
});

/* ── Arranque ──────────────────────────────────────────────────────────── */

fillLogPlatforms();
setupSitesHint();
setTab(TABS.has(location.hash.replace("#", "")) ? location.hash.replace("#", "") : "inicio");
/* Tema claro u oscuro. index.html lo aplica antes de pintar; acá solo se
   cambia y se recuerda. Sin transiciones durante el cambio: si no, cada
   botón y borde se desvanece a su ritmo y el cambio se ve sucio. */
function setTheme(theme) {
  const root = document.documentElement;
  root.classList.add("no-transitions");
  root.dataset.theme = theme;
  try { localStorage.setItem("demo-lab-theme", theme); } catch { /* sin almacenamiento */ }
  paintThemeButton();
  void root.offsetHeight;
  requestAnimationFrame(() => root.classList.remove("no-transitions"));
}
function paintThemeButton() {
  const label = document.documentElement.dataset.theme === "light" ? "Cambiar a modo oscuro" : "Cambiar a modo claro";
  $("#btn-theme").setAttribute("aria-label", label);
  $("#btn-theme").title = label;
}

hydrateIcons();
paintThemeButton();
refresh();
setInterval(refresh, 3000);
setInterval(() => { if (state.tab === "actividad") refreshActivity(); }, 3000);
setInterval(() => { if (state.tab === "datos") refreshMarathon(); }, 3000);

