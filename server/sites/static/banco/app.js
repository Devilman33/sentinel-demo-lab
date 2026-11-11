import { $, $$, esc, userAction, toast, boot, carousel, skeleton, errorState, errorText } from '../shared/common.js';
import { icon, sceneIcon, foto } from '../shared/icons.js';

const PLATFORM = 'banco';
const USER = { rut: '11.111.111-1', pass: 'demo1234', name: 'Cliente Demo' };
const store = {
  get logged() { return sessionStorage.getItem('dfb.logged') === '1'; },
  set logged(v) { sessionStorage.setItem('dfb.logged', v ? '1' : '0'); },
  get balance() { return Number(sessionStorage.getItem('dfb.balance') || 1845320); },
  set balance(v) { sessionStorage.setItem('dfb.balance', String(v)); },
  get moves() { return JSON.parse(sessionStorage.getItem('dfb.moves') || '[]'); },
  set moves(v) { sessionStorage.setItem('dfb.moves', JSON.stringify(v)); },
};
const p2 = (n) => String(n).padStart(2, '0');
const fmtDate = (d) => `${p2(d.getDate())}/${p2(d.getMonth() + 1)}/${d.getFullYear()}`;
const peso = (n) => '$ ' + Math.round(n).toLocaleString('es-CL');
const daysAgo = (n) => { const d = new Date(); d.setDate(d.getDate() - n); return fmtDate(d); };
const SEED_MOVES = [
  [1, 'Pago: café del parque', 'Of. Banca Virtual', -3900], [1, 'Traspaso a: Camila Rojas', 'Internet', -50000],
  [2, 'Pago: supermercado Los Aromos', 'Of. Banca Virtual', -48990], [3, 'Abono de sueldo', 'Sucursal', 1250000],
  [4, 'Pago tarjeta de crédito', 'Internet', -310000], [5, 'Traspaso de: Tomás Pérez', 'Internet', 80000],
  [6, 'Giro cajero automático', 'Cajero', -80000], [7, 'Pago: farmacia San Martín', 'Of. Banca Virtual', -17611],
  [8, 'Devolución de comercio', 'Internet', 14990], [9, 'Pago: Inmobiliaria Lomas', 'Internet', -420000],
  [10, 'Pago: transporte urbano', 'Of. Banca Virtual', -6100], [11, 'Traspaso a: Valentina Cerda', 'Internet', -22000],
].map(([d, g, c, m]) => ({ d: daysAgo(d), g, c, m }));
const RECIPIENTS = [['Camila Rojas · 12.345.678-9', 'Banco Don Federico · Cta. Vista'], ['Inmobiliaria Lomas SpA · 76.123.456-7', 'Banco Austral · Cta. Cte.'], ['Tomás Pérez · 15.987.654-3', 'Banco Don Federico · Cta. Cte.']];

/* Menú: cada entrada abre un panel con sus accesos. */
const MENU = [
  ['Mis productos', [['Cuentas', '#/movimientos'], ['Tarjetas de crédito', '#/movimientos'], ['Líneas de crédito', '#/home'], ['Depósitos a plazo', '#/home']]],
  ['Transferencias y pagos', [['Transferir a terceros', '#/transferir'], ['Pagar cuentas', '#/home'], ['Agenda de destinatarios', '#/transferir'], ['Pago de tarjetas', '#/home']]],
  ['Inversiones', [['Fondos mutuos', '#/home'], ['Depósitos', '#/home'], ['Simulador', '#/home'], ['Asesoría', '#/home']]],
  ['Perfil y configuración', [['Datos personales', '#/home'], ['Seguridad', '#/home'], ['Límites de transferencia', '#/home'], ['Notificaciones', '#/home']]],
  ['Mis documentos', [['Cartolas', '#/movimientos'], ['Certificados', '#/home'], ['Comprobantes', '#/movimientos']]],
  ['Mis beneficios', [['Puntos Don Federico', '#/home'], ['Descuentos', '#/home'], ['Seguros', '#/home']]],
];
const ACTIVE = { home: 'Mis productos', transferir: 'Transferencias y pagos', movimientos: 'Mis documentos' };

/* Íconos de Lucide (ver shared/icons.js). */
const NAMES = { home: 'house', chev: 'chevron-down', bell: 'bell', mail: 'mail', alert: 'triangle-alert', out: 'log-out', tag: 'tag', phone: 'phone', mobile: 'smartphone', lock: 'lock', cog: 'settings', left: 'chevron-left', right: 'chevron-right', check: 'check', user: 'user-round' };
const ic = (n, cls = '', sw) => icon(NAMES[n] || n, cls, sw);

/* Ilustraciones: fondo plano y un ícono grande de Lucide. */
const scene = (bg, inner) => `<svg viewBox="0 0 420 270" preserveAspectRatio="xMidYMid slice" aria-hidden="true"><rect width="420" height="270" fill="${bg}"/>${inner}</svg>`;
const ART = {
  viaje: scene('#dfe8ff', `<circle cx="330" cy="62" r="40" fill="#fff" opacity=".7"/><path d="M0 214c70-34 120-34 190-6s150 26 230-12v74H0z" fill="#a3b7f5"/><path d="M0 240c80-22 150-20 220 0s140 12 200-6v36H0z" fill="#295eff" opacity=".85"/>${sceneIcon('luggage', 130, 56, 150, '#002464', 1.3)}${sceneIcon('plane', 276, 40, 60, '#295eff', 1.6)}`),
  ahorro: scene('#eaf0ff', `<circle cx="210" cy="140" r="104" fill="#fff" opacity=".6"/>${sceneIcon('piggy-bank', 120, 60, 170, '#002464', 1.3)}${sceneIcon('coins', 270, 36, 70, '#295eff', 1.5)}`),
  fondo: scene('#e8eeff', `<circle cx="210" cy="140" r="104" fill="#fff" opacity=".6"/>${sceneIcon('trending-up', 120, 62, 170, '#295eff', 1.4)}${sceneIcon('percent', 290, 48, 64, '#002464', 1.5)}`),
  copa: scene('#dce6ff', `<path d="M0 230c90-26 160-24 230-4s130 8 190-12v56H0z" fill="#295eff" opacity=".8"/><circle cx="210" cy="130" r="92" fill="#fff" opacity=".7"/>${sceneIcon('ticket', 126, 56, 160, '#002464', 1.3)}${sceneIcon('gift', 288, 40, 62, '#295eff', 1.5)}`),
  seguro: scene('#e9efff', `<circle cx="76" cy="60" r="40" fill="#a3b7f5" opacity=".5"/><circle cx="210" cy="140" r="96" fill="#fff" opacity=".65"/>${sceneIcon('shield-check', 130, 56, 160, '#295eff', 1.3)}${sceneIcon('laptop', 290, 150, 64, '#002464', 1.5)}`),
  tarjetas: scene('#eef2ff', `<circle cx="210" cy="140" r="100" fill="#fff" opacity=".65"/>${sceneIcon('credit-card', 110, 70, 150, '#295eff', 1.3)}${sceneIcon('wallet', 224, 96, 110, '#002464', 1.4)}`),
  monedas: `<svg viewBox="0 0 76 76" aria-hidden="true"><circle cx="38" cy="38" r="36" fill="#eaf0ff"/>${sceneIcon('coins', 14, 14, 48, '#295eff', 1.7)}</svg>`,
  hoja: `<svg viewBox="0 0 110 100" aria-hidden="true"><circle cx="58" cy="52" r="42" fill="#e6efe9"/>${sceneIcon('leaf', 28, 22, 60, '#1b7a43', 1.6)}</svg>`,
};

Object.assign(ART, { viaje: foto('b-viaje', '', '', true), ahorro: foto('b-ahorro'), fondo: foto('b-fondo'), copa: foto('b-copa'), seguro: foto('b-seguro', 'Seguros y beneficios'), tarjetas: foto('b-tarjetas', 'Pagos con tarjeta') });
boot(PLATFORM);
const app = $('#app');

function shell(inner, active) {
  return `<header class="top"><div class="wrap"><a class="wordmark" href="#/home"><i>D</i>Banco Don Federico</a><span class="seg">Banca personas</span><span class="grow"></span>
      <a class="lnk" href="#">${ic('user')}<span>Tu ejecutivo</span></a><a class="lnk" href="#">${ic('mail')}<span>Consultas</span></a>
      <a class="lnk help" href="#">${ic('alert')}<span>Ayuda</span></a><button class="bell" aria-label="Notificaciones">${ic('bell')}</button><span class="avatar" aria-hidden="true">CD</span>
      <button class="lnk out" id="logout"><span>Cerrar sesión</span>${ic('out')}</button></div></header>
    <nav class="nav" aria-label="Principal"><div class="wrap"><a class="it home ${active === 'home' ? 'on' : ''}" href="#/home" aria-label="Inicio">${ic('home')}</a>
      ${MENU.map(([t], i) => `<button class="it ${ACTIVE[active] === t ? 'on' : ''}" data-mega="${i}" aria-expanded="false" aria-controls="mega">${t}${ic('chev', 'chev')}</button>`).join('')}
      <a class="it" href="#">${ic('tag')}Mis ofertas</a></div>
      <div class="mega" id="mega"><div class="wrap"><div class="box"></div></div></div></nav>
    <main class="page"><div class="wrap">${inner}</div></main>
    <footer class="foot"><div class="wrap cols"><div><a class="wordmark" href="#/home" style="color:var(--nav-1)"><i style="color:#fff">D</i>Don Federico</a><p class="small" style="margin-top:12px">Encuentra la app en las tiendas oficiales (ficticio).</p></div>
      <div><h2>Servicio al cliente</h2><p class="phone">${ic('phone')}<span>Desde red fija: <b class="num">000 000 00 00</b><br><span class="small">Lunes a viernes de 08:00 a 19:00 horas</span></span></p>
        <p class="phone">${ic('mobile')}<span>Desde celulares: <b class="num">+00 0 0000 0000</b></span></p>
        <p class="phone">${ic('alert')}<span>Emergencias bancarias: <b class="num">+00 0 0000 0000</b><br><span class="small">Lunes a domingo, las 24 horas</span></span></p></div>
      <div><h2 class="em">Ayuda 24 hrs</h2><a href="#">Centro de ayuda</a><a href="#">Emergencias bancarias</a><a href="#">Bloqueo de tarjeta de débito</a><a href="#">Bloqueo de tarjeta de crédito</a><a href="#">Canales de atención</a></div></div>
      <div class="legal">Infórmate sobre la garantía de los depósitos · © 2026 Banco Don Federico · Sitio ficticio de demostración · <a href="../shared/photos/creditos.html">Créditos de fotografías</a></div></footer>`;
}

/** Pinta una vista, la anima una vez y conecta menú y cierre de sesión. */
function mount(inner, active, { fresh = true } = {}) {
  app.innerHTML = shell(inner, active);
  if (fresh) $$('[data-rise]', app).forEach((el, i) => { el.classList.add('rise'); el.style.setProperty('--i', i); });
  const mega = $('#mega'), box = $('.box', mega), btns = $$('[data-mega]');
  const close = () => { mega.classList.remove('open'); btns.forEach((b) => b.setAttribute('aria-expanded', 'false')); };
  btns.forEach((b) => b.addEventListener('click', (e) => {
    e.stopPropagation();
    const open = b.getAttribute('aria-expanded') === 'true';
    close();
    if (open) return;
    const [t, links] = MENU[Number(b.dataset.mega)];
    box.innerHTML = `<h6>${t}</h6>${links.map(([l, h]) => `<a href="${h}">${l}</a>`).join('')}`;
    b.setAttribute('aria-expanded', 'true'); mega.classList.add('open');
  }));
  mega.addEventListener('click', close);
  document.addEventListener('click', close, { once: true });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); }, { once: true });
  $('#logout')?.addEventListener('click', () => { store.logged = false; location.hash = '#/login'; });
}

/* Cuenta el saldo hacia arriba al entrar (sin movimiento si el usuario lo pide). */
function countUp(el, to) {
  if (!el || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  const t0 = performance.now(), from = Math.round(to * 0.9);
  const tick = (now) => {
    const k = Math.min(1, (now - t0) / 700), e = 1 - (1 - k) ** 3;
    el.textContent = peso(from + (to - from) * e);
    if (k < 1) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

/* La tarjeta «Cuentas» pide el saldo al servicio; si no responde, lo dice y
   ofrece reintentar en vez de mostrar un saldo guardado. */
async function loadHomeBalance() {
  const box = $('#bal-box');
  if (!box) return false;
  skeleton(box, 2);
  const r = await userAction(PLATFORM, 'balance');
  if (!r.ok) {
    errorState(box, { what: 'No pudimos cargar tu saldo disponible', result: r, retry: loadHomeBalance });
    return false;
  }
  box.innerHTML = `<span>Disponible</span><b class="num" id="bal">${peso(store.balance)}</b>`;
  countUp($('#bal'), store.balance);
  return true;
}

const field = (id, label, extra = '') => `<div class="field ${extra}"><input id="${id}" name="${id}" placeholder=" " autocomplete="off"><label for="${id}">${label}</label></div>`;

/* ── Login con segundo factor ─────────────────────────────────────────── */
function loginView() {
  app.innerHTML = `<header class="top"><div class="wrap"><span class="wordmark"><i>D</i>Banco Don Federico</span><span class="seg">Banca personas</span></div></header>
    <main class="page"><div class="wrap"><form class="card login rise" id="login" novalidate><h1>Ingresa a tu banca en línea</h1>
      <p class="demo">Sitio de demostración. Usa RUT <b>${USER.rut}</b> y clave <b>${USER.pass}</b>. No ingreses datos reales.</p>
      <div class="field"><input name="rut" id="rut" placeholder=" " autocomplete="off"><label for="rut">RUT</label></div>
      <div class="field"><input name="pass" id="pass" type="password" placeholder=" " autocomplete="off"><label for="pass">Clave</label></div>
      <div id="msg" aria-live="polite"></div><button class="btn solid">Ingresar</button>
      <p class="fine">Tu clave es personal e intransferible.</p></form></div></main>`;
  $('#login').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target, msg = $('#msg');
    if (f.rut.value.trim() !== USER.rut || f.pass.value !== USER.pass) { msg.innerHTML = '<div class="err-box">Solo se aceptan las credenciales de demostración.</div>'; return; }
    const btn = f.querySelector('button'); btn.disabled = true; btn.textContent = 'Validando segundo factor…';
    const r = await userAction(PLATFORM, 'login');
    if (r.ok) { store.logged = true; location.hash = '#/home'; }
    else { msg.innerHTML = `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos validar tu clave'))}</strong>El servicio de autenticación no está disponible. Intenta nuevamente en unos minutos.</div>`; btn.disabled = false; btn.textContent = 'Ingresar'; }
  };
}

/* ── Home ─────────────────────────────────────────────────────────────── */
const SLIDES = [
  ['Tu seguro de viaje, listo en minutos', 'Contrata ahora y viaja protegido, con cobertura médica y de equipaje.', 'Hazlo aquí', ART.viaje, 'Seguro intermediado por una corredora ficticia. Sitio de demostración.'],
  ['Cuenta de ahorro sin costos de mantención', 'Abre tu cuenta 100% en línea y empieza a ahorrar con tasa preferente.', 'Solicitar', ART.ahorro, ''],
  ['Revisa el desempeño de tu fondo', 'Conoce sus resultados y su evolución en este video corto.', 'Ver video', ART.fondo, ''],
  ['Participa por entradas al partido', 'Inscríbete con tu tarjeta y entra al sorteo de entradas dobles.', 'Inscríbete', ART.copa, ''],
];
function homeView() {
  const today = new Date().toLocaleDateString('es-CL', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  mount(`
    <h1 class="sr">Inicio</h1>
    <section class="hero">
      <div class="stack">
        <div class="card sustain" data-rise><p class="eyebrow">Cuidemos juntos</p><h2>El futuro es digital</h2><p>Prefiere la comunicación digital y digitaliza tu correspondencia.</p><a class="btn" href="#">Súmate aquí</a><span class="art">${ART.hoja}</span></div>
        <div class="renta" data-rise><span>${ART.monedas}</span><div><h2>Actualiza tu renta</h2><p>En 2 simples pasos recibe mejores ofertas a tu medida.</p><a class="btn" href="#">Actualizar renta</a></div></div>
      </div>
      <div data-rise id="cw"><div class="slides">${SLIDES.map(([h, p, b, art, fine]) => `<article class="slide"><div class="txt"><h2>${h}</h2><p>${p}</p><a class="btn solid" href="#">${b}</a>${fine ? `<span class="fine">${fine}</span>` : ''}</div><div class="img">${art}</div></article>`).join('')}</div>
        <div class="cbar"><div class="dots"></div><div class="arrows"><button class="prev" aria-label="Anterior">${ic('left')}</button><button class="next" aria-label="Siguiente">${ic('right')}</button></div></div></div>
    </section>
    <section class="card shortcuts" data-rise><h2>Accesos directos<span class="sub"><a class="btn ghost dark" href="#">${ic('cog')} Personalizar</a></span></h2>
      <div class="row"><a class="btn ghost warn" href="#">${ic('alert')} Emergencias</a><a class="btn ghost" href="#/transferir">Transferir a terceros</a><a class="btn ghost" href="#/movimientos">Saldos y mov. cuentas</a><a class="btn ghost" href="#/movimientos">Saldos y mov. tarjetas</a></div></section>
    <section class="grid3" data-rise>
      <div><div class="sechead"><h2>Cuentas</h2><a class="btn ghost dark" href="#" id="refresh-bal">Actualizar saldos</a></div>
        <div class="card acct"><div class="t"><b>Cuenta corriente</b><span class="num n">00-000-00000-00</span><a class="btn" href="#/movimientos">Ver cuenta</a></div><div class="bal" id="bal-box"><span>Disponible</span><span class="sk"></span></div></div></div>
      <div><div class="sechead"><h2>Tarjetas de crédito</h2><a class="btn ghost dark" href="#/movimientos">Ver saldos</a></div><div class="plastic"><span class="chip"></span><span class="arc"></span><span class="num">•••• 0691</span><b>Adicional Platinum</b></div></div>
      <div class="points"><h2>Puntos Don Federico</h2><p>¿Quieres canjear tus puntos?</p><a class="btn" href="#">Revise aquí</a><br><a class="btn ghost dark" href="#">Conoce más</a></div>
    </section>
    <section class="grid3" style="margin-top:56px" data-rise><div class="card safety">${ic('lock', 'ico')}<h2>Tu seguridad es lo más importante</h2><p>Para protegerte, ten siempre en cuenta estos útiles consejos.</p><a class="btn solid" href="#" style="height:32px">Más información</a></div>
      <div class="interest"><div class="sechead"><h2>Te puede interesar</h2></div><div class="two"><a class="card tile" href="#"><div class="art">${ART.seguro}</div><p>Simula y contrata el seguro que más necesites</p></a><a class="card tile" href="#"><div class="art">${ART.tarjetas}</div><p>Disfruta los beneficios de tus tarjetas de crédito y débito</p></a></div></div></section>
    <p class="ind" data-rise>Indicadores · ${today.charAt(0).toUpperCase() + today.slice(1)} <b>Dólar</b>994,29 <b>Euro</b>1.112,8 <b>UF</b>41.226,39 <b>UTM</b>72.151</p>`, 'home');
  carousel($('#cw'), 6000);
  loadHomeBalance();
  $('#refresh-bal')?.addEventListener('click', async (e) => {
    e.preventDefault();
    if (await loadHomeBalance()) toast('Saldos actualizados', 'good');
  });
}

/* ── Movimientos ──────────────────────────────────────────────────────── */
async function movesView() {
  mount(`<h1 class="t" data-rise>Consultar cuentas</h1><p data-rise style="margin:12px 0 0">Cuenta N°: <span class="num">00-000-00000-00</span> (CLP)</p>
    <nav class="tabs" data-rise aria-label="Consultas"><a class="on" href="#/movimientos">Saldos y movimientos</a><a href="#">Cartola histórica</a><a href="#">Cartola de intereses</a></nav><div id="mv"><div class="card">Consultando tu cuenta…</div></div>`, 'movimientos');
  skeleton($('#mv'), 4);
  const r = await userAction(PLATFORM, 'balance');
  if (!r.ok) {
    errorState($('#mv'), { what: 'No pudimos consultar tus movimientos', result: r, retry: movesView });
    return;
  }
  let saldo = store.balance;
  const rows = [...store.moves, ...SEED_MOVES].map((m) => { const o = { ...m, s: saldo }; saldo -= m.m; return o; });
  const cargos = rows.filter((m) => m.m < 0).reduce((a, m) => a - m.m, 0), abonos = rows.filter((m) => m.m > 0).reduce((a, m) => a + m.m, 0);
  const hoy = new Date();
  $('#mv').innerHTML = `<div class="summary rise"><h2>Saldo al ${fmtDate(hoy)} ${p2(hoy.getHours())}:${p2(hoy.getMinutes())} Hrs.</h2>
      <div class="dl"><p class="lead">Saldo disponible cuenta corriente</p><b class="num big">${peso(store.balance)}</b><div class="li"><span>Saldo contable</span><b class="num">${peso(store.balance)}</b></div><div class="li"><span>Retenciones 24h</span><b class="num">$0</b></div></div>
      <div class="dl" style="align-self:end"><div class="li"><span>Total cargos</span><b class="num">${peso(cargos)}</b></div><div class="li"><span>Total abonos</span><b class="num">${peso(abonos)}</b></div></div></div>
    <span class="chip rise" style="--i:1">Movimientos de los últimos 30 días</span><p class="sub-note rise" style="--i:1">La búsqueda por intervalos de fechas permite consultar hasta 90 días hacia atrás.</p>
    <div class="filters rise" style="--i:2"><div class="field" style="width:min(595px,100%);margin:0"><input id="q" placeholder=" " autocomplete="off"><label for="q">Buscar</label></div><a class="btn" href="#" id="dl">Descargar</a></div>
    <div class="tablewrap rise" style="--i:3"><table class="mv"><thead><tr><th>Fecha</th><th>Descripción</th><th>Canal o sucursal</th><th class="r">Cargos (CLP)</th><th class="r">Abonos (CLP)</th><th class="r">Saldo (CLP)</th></tr></thead><tbody id="rows"></tbody></table></div>`;
  const paint = (q = '') => {
    const t = q.trim().toLowerCase(), sel = rows.filter((m) => !t || `${m.g} ${m.c} ${m.d}`.toLowerCase().includes(t));
    $('#rows').innerHTML = sel.length ? sel.map((m) => `<tr><td>${esc(m.d)}</td><td>${esc(m.g)}</td><td>${esc(m.c)}</td><td class="r neg">${m.m < 0 ? peso(-m.m) : ''}</td><td class="r pos">${m.m > 0 ? peso(m.m) : ''}</td><td class="r">${peso(m.s)}</td></tr>`).join('') : '<tr><td colspan="6" class="empty">No encontramos movimientos con esa búsqueda.</td></tr>';
  };
  paint();
  $('#q').addEventListener('input', (e) => paint(e.target.value));
  $('#dl').addEventListener('click', (e) => { e.preventDefault(); toast('Cartola descargada (demostración)', 'good'); });
}

/* ── Transferencias: pasos 1 a 4 ──────────────────────────────────────── */
function transferView() {
  const t = { step: 2, dest: '', amount: '', msg: '', mail: '', errDest: false, errAmt: '' };
  mount(`<h1 class="t" data-rise>Transferencias</h1>
    <nav class="tabs t24" data-rise><a class="on" href="#/transferir">Transferencias a terceros</a><a href="#">Consulta de transferencias</a><a href="#">Agenda</a><a href="#">Envío de dinero</a></nav>
    <div class="notice" data-rise><p><b>Porque nos preocupamos de tu seguridad</b>, al crear un destinatario nuevo o editar sus datos, ten presente que se aplican límites transitorios de transferencia durante las primeras horas, según lo siguiente:</p>
      <p>• Para transferencias a cuentas de otros bancos, durante las primeras 12 horas desde que se crea o se modifica un destinatario regirá un límite acumulado de $350.000 por banco de destino, sin importar la cantidad de operaciones.</p>
      <p>• Para transferencias en cuenta corriente o vista del mismo banco, durante las primeras 12 horas el límite acumulado por destinatario nuevo o modificado será de $1.000.000. Pasado ese plazo rigen los límites habituales.</p></div>
    <div class="steps" id="steps" data-rise></div>`, 'transferir');
  const formStep = () => `<div class="form"><div>
      <div class="field"><select id="origin"><option>Cuenta corriente 00-000-00000-00</option></select><label for="origin">Cuenta de origen</label></div>
      <div class="field ${t.errDest ? 'bad' : ''}"><select id="dest"><option value="">Selecciona un destinatario</option>${RECIPIENTS.map(([n, b]) => `<option ${t.dest === n ? 'selected' : ''} value="${esc(n)}">${esc(n)} — ${esc(b)}</option>`).join('')}</select><label for="dest">Destinatario</label><span class="hint">Busca por nombre, RUT o N° de cuenta</span>${t.errDest ? '<span class="errm">Selecciona un destinatario.</span>' : ''}</div>
      <div class="field ${t.errAmt ? 'bad' : ''}"><input id="amount" inputmode="numeric" value="${esc(t.amount)}" placeholder=" " autocomplete="off"><label for="amount">Monto a transferir</label>${t.errAmt ? `<span class="errm">${t.errAmt}</span>` : ''}</div>
      <div class="field"><textarea id="msg" maxlength="50" placeholder=" ">${esc(t.msg)}</textarea><label for="msg">Mensaje</label><span class="count" id="count">${t.msg.length}/50</span></div>
      <div class="field"><input id="mail" value="${esc(t.mail)}" placeholder=" " autocomplete="off"><label for="mail">Enviar comprobante a</label><span class="hint">usuario@ejemplo.cl</span></div>
      <div class="actions"><a class="btn" href="#/home">Cancelar</a><button class="btn solid" id="next" disabled>Continuar</button></div></div>
      <div class="balbox" id="balbox"><span>Disponible</span><span class="sk"></span></div></div>`;
  const confirmStep = () => `<dl class="summary-list"><dt>Destinatario</dt><dd>${esc(t.dest)}</dd><dt>Monto</dt><dd class="num">${peso(Number(t.amount))}</dd><dt>Mensaje</dt><dd>${esc(t.msg) || '—'}</dd><dt>Comprobante a</dt><dd>${esc(t.mail) || '—'}</dd></dl>
      <div class="actions"><button class="btn" id="back">Modificar</button><button class="btn solid" id="next">Confirmar datos</button></div>`;
  const otpStep = () => `<p style="margin:0;font-size:14px">Ingresa los 6 dígitos de tu dispositivo de seguridad</p><div class="otp">${[0, 1, 2, 3, 4, 5].map((i) => `<input maxlength="1" inputmode="numeric" class="otp-d" aria-label="Dígito ${i + 1}">`).join('')}</div>
      <div id="result" aria-live="polite"></div><div class="actions"><button class="btn" id="back">Volver</button><button class="btn solid" id="send">Transferir</button></div>`;
  const loadTransferBalance = async () => {
    const box = $('#balbox');
    if (!box) return false;
    skeleton(box, 2);
    const btn = $('#next');
    if (btn) btn.disabled = true;
    const r = await userAction(PLATFORM, 'balance');
    if (!r.ok) {
      errorState(box, { what: 'No pudimos cargar tu saldo disponible', result: r, retry: loadTransferBalance });
      return false;
    }
    box.innerHTML = `<span>Disponible</span><b class="num main">${peso(store.balance)}</b><span>Total disponible</span><b class="num">${peso(store.balance)}</b>`;
    if ($('#next')) $('#next').disabled = false;
    return true;
  };
  const draw = (focus = true) => {
    const s = t.step, cls = (n) => (s > n ? 'done' : s >= n ? '' : 'off'), body = (n, html) => (s === n ? `<div class="body">${html}</div>` : '');
    $('#steps').innerHTML = `
      <div class="step ${cls(1)}"><span class="n">${s > 1 ? ic('check') : 1}</span><h2>¿Cuándo deseas realizar la transferencia?</h2>${s === 1 ? '' : ''}
        <label class="radio"><input type="radio" name="when" checked> Inmediata</label><label class="radio"><input type="radio" name="when" disabled> Programada</label></div>
      <div class="step ${cls(2)}"><span class="n">${s > 2 ? ic('check') : 2}</span><h2>Completa los datos de transferencia</h2>${body(2, formStep())}</div>
      <div class="step ${cls(3)}"><span class="n">${s > 3 ? ic('check') : 3}</span><h2>Confirma los datos ingresados</h2>${body(3, confirmStep())}</div>
      <div class="step ${cls(4)}"><span class="n">4</span><h2>Ingresa tu dispositivo de seguridad</h2>${body(4, otpStep())}</div>`;
    $$('.n svg', app).forEach((v) => { v.style.width = '18px'; v.style.height = '18px'; v.style.strokeWidth = '2.4'; });
    bind();
    if (t.step === 2) loadTransferBalance();
    if (focus) $('.body input:not([disabled]), .body select, .body #next', app)?.focus({ preventScroll: true });
  };
  const bind = () => {
    $('#back')?.addEventListener('click', () => { t.step -= 1; draw(); });
    $('#next')?.addEventListener('click', () => {
      if (t.step === 2) {
        t.dest = $('#dest').value; t.amount = $('#amount').value.replace(/\D/g, ''); t.msg = $('#msg').value; t.mail = $('#mail').value;
        t.errDest = !t.dest; const n = Number(t.amount);
        t.errAmt = !n ? 'Ingresa un monto.' : n > store.balance ? 'El monto supera tu saldo disponible.' : '';
        if (t.errDest || t.errAmt) { draw(false); return; }
      }
      t.step += 1; draw();
    });
    $('#amount')?.addEventListener('input', (e) => { e.target.value = e.target.value.replace(/\D/g, ''); });
    $('#msg')?.addEventListener('input', (e) => { $('#count').textContent = `${e.target.value.length}/50`; });
    $$('.otp-d').forEach((el, i, all) => {
      el.addEventListener('input', () => { el.value = el.value.replace(/\D/g, ''); if (el.value && all[i + 1]) all[i + 1].focus(); });
      el.addEventListener('keydown', (e) => { if (e.key === 'Backspace' && !el.value && all[i - 1]) all[i - 1].focus(); });
    });
    $('#send')?.addEventListener('click', async () => {
      const code = $$('.otp-d').map((x) => x.value).join('');
      if (code.length !== 6) { $('#result').innerHTML = '<div class="err-box">Ingresa los 6 dígitos de tu clave dinámica.</div>'; return; }
      const btn = $('#send'); btn.disabled = true; btn.textContent = 'Validando con antifraude…';
      const r = await userAction(PLATFORM, 'transfer');
      if (r.ok) {
        const n = Number(t.amount); store.balance = store.balance - n;
        store.moves = [{ d: fmtDate(new Date()), g: 'Traspaso a: ' + t.dest.split(' · ')[0], c: 'Internet', m: -n }, ...store.moves];
        $('#result').innerHTML = `<div class="ok-box">${ic('check')}<div><strong>Transferencia realizada</strong>Enviaste ${peso(n)}. Comprobante N° ${esc(r.receipt)} · ${Math.round(r.ms)} ms</div></div>`;
        btn.hidden = true; $('#back').hidden = true;
      } else {
        btn.disabled = false; btn.textContent = 'Transferir';
        $('#result').innerHTML = `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos realizar tu transferencia'))}</strong>No se realizó ningún cargo en tu cuenta.</div>`;
      }
    });
  };
  draw(false);
}

/* ── Enrutador ────────────────────────────────────────────────────────── */
function route() {
  const h = location.hash.replace('#/', '') || 'home';
  if (!store.logged) return loginView();
  ({ home: homeView, movimientos: movesView, transferir: transferView }[h] || homeView)();
  scrollTo(0, 0);
}
addEventListener('hashchange', route);
route();
