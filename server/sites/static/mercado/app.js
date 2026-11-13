import { $, $$, esc, userAction, toast, carousel, boot, skeleton, emptyState, errorState, errorText } from '../shared/common.js';
import { icon, foto } from '../shared/icons.js';

const PLATFORM = 'mercado';
const peso = (n) => '$ ' + Math.round(n).toLocaleString('es-CL');

/* Íconos de Lucide (ver shared/icons.js): un ícono por producto sobre un fondo de su categoría. */
const TINT = {
  'Frutas y verduras': ['#e9f6e4', '#c8372d'], 'Carnes y pescados': ['#fdecea', '#b83a32'], 'Lácteos': ['#eaf2fb', '#3f78c4'], 'Despensa': ['#fbf1dc', '#b87a1f'],
  'Panadería': ['#fbefe0', '#c0813a'], 'Bebidas': ['#e7f6f6', '#12899c'], 'Limpieza': ['#efe9fb', '#6a49c4'], 'Congelados': ['#e8f1fb', '#4a89d4'],
};
const CAT_ICON = { 'Frutas y verduras': 'apple', 'Carnes y pescados': 'beef', 'Lácteos': 'milk', 'Despensa': 'wheat', 'Panadería': 'croissant', 'Bebidas': 'cup-soda', 'Limpieza': 'spray-can', 'Congelados': 'ice-cream-cone' };
const pic = (p) => foto('m-' + p.id, p.name, 'cut');
/* Un producto de catálogo representa cada categoría; portadas y categorías se arman con los mismos recortes. */
const TILE_PROD = { 'Frutas y verduras': 'p3', 'Carnes y pescados': 'p17', Lácteos: 'p4', Despensa: 'p5', Panadería: 'p2', Bebidas: 'p8', Limpieza: 'p16', Congelados: 'p14' };
const HERO_PROD = { despensa: ['p2', 'p5', 'p4'], frescos: ['p3', 'p1', 'p18'], club: ['p13', 'p9', 'p6'] };
$$('[data-art]').forEach((el) => { el.innerHTML = '<div class="cutouts">' + HERO_PROD[el.dataset.art].map((id) => foto('m-' + id, '', 'cut', el.dataset.art === 'despensa')).join('') + '</div>' + (el.dataset.art === 'club' ? '<span class="cupon">DON10</span>' : ''); });

const CATEGORIES = ['Frutas y verduras', 'Carnes y pescados', 'Lácteos', 'Despensa', 'Panadería', 'Bebidas', 'Limpieza', 'Congelados'];
// id, categoría, marca, nombre, precio, precio anterior (0 = sin oferta)
const PRODUCTS = [
  ['p1', 'Frutas y verduras', 'Campo Fresco', 'Plátanos 1 kg', 1490, 1790], ['p2', 'Panadería', 'Horno Sur', 'Pan de molde familiar', 2490, 0],
  ['p3', 'Frutas y verduras', 'Campo Fresco', 'Manzanas rojas 1 kg', 2290, 2690], ['p4', 'Lácteos', 'Granja Sur', 'Huevos de campo 12 un.', 3290, 0],
  ['p5', 'Despensa', 'Altiplano', 'Café en grano 250 g', 4990, 5990], ['p6', 'Frutas y verduras', 'Campo Fresco', 'Naranjas 1 kg', 1890, 2190],
  ['p7', 'Frutas y verduras', 'Campo Fresco', 'Zanahorias 1 kg', 990, 0], ['p8', 'Bebidas', 'Fuente Clara', 'Agua mineral 1,5 L', 990, 0],
  ['p9', 'Frutas y verduras', 'Campo Fresco', 'Palta Hass 1 kg', 4490, 4990], ['p10', 'Lácteos', 'Valle Verde', 'Yogurt natural 1 kg', 2190, 0],
  ['p11', 'Despensa', 'Granero', 'Fideos espagueti 400 g', 990, 1290], ['p12', 'Bebidas', 'Colmena', 'Té frío de frutas 5 L', 5490, 6290],
  ['p13', 'Frutas y verduras', 'Campo Fresco', 'Uvas verdes 500 g', 2990, 0], ['p14', 'Congelados', 'Polar', 'Conos de helado x6', 3290, 3890],
  ['p15', 'Panadería', 'Horno Sur', 'Marraqueta (6 un.)', 1690, 0], ['p16', 'Limpieza', 'Brisa', 'Papel higiénico 12 rollos', 5490, 6290],
  ['p17', 'Carnes y pescados', 'Mar Austral', 'Filete de salmón 500 g', 7490, 8490], ['p18', 'Frutas y verduras', 'Campo Fresco', 'Tomates 1 kg', 1990, 0],
].map(([id, cat, brand, name, price, old], i) => ({ id, cat, brand, name, price, old, rate: Math.min(5, 3.4 + ((i * 7) % 17) / 10).toFixed(1) }));
const SHELVES = [
  ['Lo más vendido', (p) => p.old > 0 || ['p2', 'p4', 'p8'].includes(p.id)],
  ['Ofertas exclusivas online', (p) => ['Despensa', 'Bebidas', 'Limpieza'].includes(p.cat)],
  ['Frescos para hoy', (p) => ['Frutas y verduras', 'Carnes y pescados', 'Lácteos', 'Panadería'].includes(p.cat)],
];
const COUPON = 'DON10';
const state = { cart: {}, couponOn: false, filter: '', cat: '', user: null };

boot(PLATFORM);
carousel($('#hero'), 6000);

/* ── Cabecera: ubicación, categorías y Club ───────────────────────────── */
const whereBtn = $('#where-btn'), wherePop = $('#where-pop'), catsBtn = $('#cats-all'), mega = $('#mega');
const closeMenus = () => { wherePop.classList.remove('open'); whereBtn.setAttribute('aria-expanded', 'false'); mega.classList.remove('open'); catsBtn.setAttribute('aria-expanded', 'false'); };
whereBtn.addEventListener('click', (e) => { e.stopPropagation(); const o = !wherePop.classList.contains('open'); closeMenus(); wherePop.classList.toggle('open', o); whereBtn.setAttribute('aria-expanded', String(o)); });
wherePop.addEventListener('click', (e) => e.stopPropagation());
$('#comuna').addEventListener('change', (e) => { $('#where-label').textContent = 'Despacho a ' + e.target.value; closeMenus(); toast('Despacho a ' + e.target.value, 'good'); });
$('#mega-box').innerHTML = CATEGORIES.map((c) => `<a href="#" data-cat="${c}"><span class="mi" style="background:${TINT[c][0]};color:${TINT[c][1]}">${icon(CAT_ICON[c], '', 1.6)}</span>${c}</a>`).join('');
catsBtn.addEventListener('click', (e) => { e.stopPropagation(); const o = !mega.classList.contains('open'); closeMenus(); mega.classList.toggle('open', o); catsBtn.setAttribute('aria-expanded', String(o)); });
document.addEventListener('click', closeMenus);
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') { closeMenus(); openDrawer(false); } if ((e.key === 'Enter' || e.key === ' ') && e.target.matches?.('.tile')) { e.preventDefault(); e.target.click(); } });
$('#club').onclick = () => toast('Para unirte al Club, inicia sesión (demostración)');
$('#club-cta').onclick = (e) => { e.preventDefault(); toast('Club Don Federico: usa el cupón DON10 en tu carrito', 'good'); };

function paintTiles() {
  $('#tiles').innerHTML = CATEGORIES.map((c) => `<div class="tile" data-cat="${c}" role="button" tabindex="0"><div class="ph" style="background:${TINT[c][0]}">${foto('m-' + TILE_PROD[c], '', 'cut')}</div><h3>${c}</h3></div>`).join('');
}

/* ── Catálogo ─────────────────────────────────────────────────────────── */
const act = (p) => {
  const q = state.cart[p.id] || 0;
  return q ? `<div class="stepper"><button data-dec="${p.id}" aria-label="Quitar uno">−</button><b>${q}</b><button data-inc="${p.id}" aria-label="Sumar uno">+</button></div>` : `<button class="add" data-inc="${p.id}">Agregar</button>`;
};
const card = (p) => `<article class="card" data-card="${p.id}"><div class="tag ${p.old ? '' : 'none'}">Oferta</div><div class="pimg" style="--bg:${TINT[p.cat][0]};color:${TINT[p.cat][1]}">${pic(p)}</div>
  <div class="price">${peso(p.price)}${p.old ? `<span class="old">${peso(p.old)}</span>` : ''}</div><span class="save ${p.old ? '' : 'none'}">${p.old ? 'Ahorras ' + peso(p.old - p.price) : '&nbsp;'}</span>
  <div class="brand">${esc(p.brand)}</div><h3 class="name"><a href="#/producto/${p.id}">${esc(p.name)}</a></h3><div class="act">${act(p)}</div>
  <div class="rate">${icon('star', '', 1.5)}${p.rate}</div></article>`;
const ARROWS = `<button class="sh-arrow l" aria-label="Anterior" data-scroll="-1">${icon('chevron-left')}</button><button class="sh-arrow r" aria-label="Siguiente" data-scroll="1">${icon('chevron-right')}</button>`;
function renderShelves() {
  const match = (p) => (!state.cat || p.cat === state.cat) && (!state.filter || (p.name + p.brand + p.cat).toLowerCase().includes(state.filter));
  const filtered = state.cat || state.filter;
  const shelves = filtered ? [[state.cat || `Resultados para «${state.filter}»`, () => true]] : SHELVES;
  const parts = shelves.map(([title, fn]) => {
    const items = PRODUCTS.filter((p) => match(p) && fn(p));
    return `<div class="shelf-head"><h2>${esc(title)}${filtered ? `<small class="count">${items.length} ${items.length === 1 ? 'producto' : 'productos'}</small>` : ''}</h2>${filtered ? '<a href="#" data-clear>Ver todo</a>' : ''}</div>` +
      (items.length ? (filtered ? `<div class="row grid">${items.map(card).join('')}</div>` : `<div class="shelf-wrap">${ARROWS}<div class="row">${items.map(card).join('')}</div></div>`) : '<div class="empty">No encontramos productos con ese criterio.</div>');
  });
  $('#shelves').innerHTML = parts[0]; $('#shelves-b').innerHTML = parts.slice(1).join('');
}
/** Actualiza solo la acción de la tarjeta (Agregar ↔ contador) para no perder la posición de cada estante. */
function refreshCard(id) {
  const p = PRODUCTS.find((x) => x.id === id);
  $$(`[data-card="${id}"] .act`).forEach((el) => { el.innerHTML = act(p); });
}

/* El catálogo depende de api-catalogo: cada vista de productos (portada,
   categoría, búsqueda, «Ver todo» y ficha) lo consulta al mostrarse y, si no
   responde, muestra el error en lugar de productos guardados. */
const viewKey = () => [location.hash, state.cat, state.filter].join('|');

async function showShelves() {
  const key = viewKey(), filtered = Boolean(state.cat || state.filter);
  $$('.promos, .featured').forEach((el) => { el.hidden = filtered; });
  skeleton($('#shelves'), 3);
  if (filtered) $('#shelves-b').innerHTML = ''; else skeleton($('#shelves-b'), 2);
  if (!$('#tiles .tile')) skeleton($('#tiles'), 2);
  const r = await userAction(PLATFORM, 'browse');
  if (viewKey() !== key) return; // el visitante ya pasó a otra vista
  if (!r.ok) {
    const what = state.filter ? 'No pudimos buscar productos' : state.cat ? `No pudimos cargar la categoría ${state.cat}` : 'No pudimos cargar los productos';
    errorState($('#shelves'), { what, result: r, retry: showShelves });
    $('#shelves-b').innerHTML = '';
    errorState($('#tiles'), { what: 'No pudimos cargar las categorías', result: r, retry: showShelves });
    return;
  }
  paintTiles();
  renderShelves();
}

/** Lleva a los estantes con el filtro actual; desde una ficha, el cambio de ruta los dibuja. */
function openShelves() {
  if (location.hash.startsWith('#/producto')) location.hash = '#/'; else showShelves();
}

$('#search').addEventListener('submit', (e) => {
  e.preventDefault();
  state.filter = $('#q').value.trim().toLowerCase(); state.cat = '';
  openShelves();
});
document.addEventListener('click', (e) => {
  const sc = e.target.closest('[data-scroll]');
  if (sc) { const box = sc.parentElement.querySelector('.row, .tiles'); box.scrollBy({ left: Number(sc.dataset.scroll) * 456, behavior: 'smooth' }); return; }
  const catEl = e.target.closest('[data-cat]');
  if (catEl) { e.preventDefault(); state.cat = catEl.dataset.cat; state.filter = ''; closeMenus(); openShelves(); $('#shelves').scrollIntoView({ behavior: 'smooth' }); return; }
  const cardEl = e.target.closest('.card');
  if (cardEl && !e.target.closest('button, a')) { location.hash = '#/producto/' + cardEl.dataset.card; return; }
  if (e.target.closest('[data-clear]')) { e.preventDefault(); state.cat = state.filter = ''; $('#q').value = ''; showShelves(); return; }
  const inc = e.target.closest('[data-inc]'), dec = e.target.closest('[data-dec]');
  if (inc) addToCart(inc.dataset.inc, inc); else if (dec) setQty(dec.dataset.dec, -1);
});

/* ── Carrito ──────────────────────────────────────────────────────────── */
async function addToCart(id, btn) {
  if (btn) btn.disabled = true;
  const r = await userAction(PLATFORM, 'cart_add');
  if (btn) btn.disabled = false;
  if (!r.ok) { toast(errorText(r, 'No pudimos agregar el producto'), 'bad'); return; }
  setQty(id, 1);
}
function setQty(id, d) {
  state.cart[id] = Math.max(0, (state.cart[id] || 0) + d);
  if (!state.cart[id]) delete state.cart[id];
  refreshCard(id); renderCart();
  $(`[data-card="${id}"] [data-inc], #lines [data-inc="${id}"]`)?.focus({ preventScroll: true });
}
function totals() {
  const lines = Object.entries(state.cart).map(([id, q]) => ({ p: PRODUCTS.find((x) => x.id === id), q }));
  const sub = lines.reduce((a, l) => a + l.p.price * l.q, 0);
  const disc = state.couponOn ? Math.round(sub * 0.1) : 0;
  const ship = sub === 0 || sub - disc >= 30000 ? 0 : 2990;
  return { lines, sub, disc, ship, total: sub - disc + ship };
}
let lastCount = 0;
function renderCart() {
  const t = totals(), count = t.lines.reduce((a, l) => a + l.q, 0), badge = $('#cart-count');
  badge.textContent = count; $('#cart-total').textContent = peso(t.sub - t.disc);
  if (count > lastCount) { badge.classList.remove('bump'); void badge.offsetWidth; badge.classList.add('bump'); }
  lastCount = count;
  $('#lines').innerHTML = t.lines.length ? t.lines.map(({ p, q }) => `<li><span class="a" style="--bg:${TINT[p.cat][0]}">${pic(p)}</span>
    <div>${esc(p.name)}<br><small>${esc(p.brand)} · ${peso(p.price)}</small><div class="stepper"><button data-dec="${p.id}" aria-label="Quitar uno">−</button><b>${q}</b><button data-inc="${p.id}" aria-label="Sumar uno">+</button></div></div>
    <b>${peso(p.price * q)}</b></li>`).join('') : '<li class="empty">Tu carrito está vacío.</li>';
  $('#s-sub').textContent = peso(t.sub); $('#s-ship').textContent = t.ship ? peso(t.ship) : 'Gratis';
  $('#s-disc-l').hidden = $('#s-disc').hidden = !t.disc; $('#s-disc').textContent = '−' + peso(t.disc);
  $('#s-total').textContent = peso(t.total);
  $('#pay').textContent = `Pagar pedido (${peso(t.total)})`; $('#pay').disabled = !t.lines.length;
}
function openDrawer(open) {
  $('#drawer').classList.toggle('open', open); $('#scrim').classList.toggle('open', open); $('#drawer').setAttribute('aria-hidden', String(!open));
  if (open) $('#drawer-close').focus({ preventScroll: true }); else if (document.activeElement && $('#drawer').contains(document.activeElement)) $('#btn-cart').focus({ preventScroll: true });
}
$('#btn-cart').onclick = () => openDrawer(true);
$('#drawer-close').onclick = $('#scrim').onclick = () => openDrawer(false);
function applyCoupon(code) {
  state.couponOn = code.trim().toUpperCase() === COUPON;
  toast(state.couponOn ? 'Cupón aplicado: 10% de descuento' : 'Cupón inválido', state.couponOn ? 'good' : 'bad'); renderCart();
}
$('#apply-coupon').onclick = () => applyCoupon($('#coupon').value);
$('#hero-coupon').onclick = () => { applyCoupon(COUPON); $('#coupon').value = COUPON; };

$('#pay').onclick = async () => {
  const out = $('#pay-result'), btn = $('#pay');
  btn.disabled = true; btn.textContent = 'Procesando pago…'; out.innerHTML = '';
  const r = await userAction(PLATFORM, 'checkout');
  if (r.ok) {
    out.innerHTML = `<div class="ok-box"><strong>¡Pago aprobado!</strong>Tu pedido está en preparación. Comprobante N° ${esc(r.receipt)} · ${Math.round(r.ms)} ms</div>`;
    const ids = Object.keys(state.cart); state.cart = {}; state.couponOn = false; ids.forEach(refreshCard);
    const html = out.innerHTML; renderCart(); out.innerHTML = html; btn.textContent = 'Pagar pedido';
  } else {
    out.innerHTML = `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos procesar tu pago'))}</strong>Tu tarjeta no fue cobrada; intenta nuevamente en unos minutos.</div>`;
    renderCart();
  }
};

/* ── Sesión (solo credenciales demo, nada sale del módulo) ────────────── */
$('#btn-login').onclick = () => {
  if (state.user) { state.user = null; $('#login-label').textContent = 'Iniciar sesión'; toast('Sesión cerrada'); return; }
  const back = document.createElement('div');
  back.className = 'modal-back';
  const inp = 'width:100%;height:42px;padding:0 12px;margin:4px 0 12px;border:1px solid #c9ced3;border-radius:12px;font:inherit';
  back.innerHTML = `<form class="modal"><h2>Ingresa a tu cuenta</h2>
    <p style="color:#666;font-size:14px;line-height:20px">Sitio de demostración: usa <b>cliente@demo.cl</b> / <b>demo1234</b>. No ingreses datos reales.</p>
    <label>Correo<br><input name="u" value="cliente@demo.cl" style="${inp}"></label>
    <label>Clave<br><input name="p" type="password" value="demo1234" style="${inp}"></label>
    <div id="login-msg"></div><button class="add">Ingresar</button></form>`;
  document.body.append(back);
  back.addEventListener('click', (e) => { if (e.target === back) back.remove(); });
  back.querySelector('button').focus();
  back.querySelector('form').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target, msg = back.querySelector('#login-msg');
    if (f.u.value !== 'cliente@demo.cl' || f.p.value !== 'demo1234') { msg.innerHTML = '<div class="err-box">Solo se aceptan las credenciales de demostración.</div>'; return; }
    const r = await userAction(PLATFORM, 'login');
    if (r.ok) { state.user = f.u.value; $('#login-label').textContent = 'Hola, Cliente'; back.remove(); toast('Sesión iniciada', 'good'); }
    else msg.innerHTML = `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos iniciar tu sesión'))}</strong>Intenta más tarde.</div>`;
  };
};
/* ── Ficha de producto (ruta #/producto/ID) ───────────────────────────── */
const HOME = ['#hero', '.club-banner', '#shelves', '.promos', '.featured', '#shelves-b'];
const DESC = { Lácteos: 'Producto fresco, sin conservantes agregados. Mantener refrigerado entre 1 °C y 5 °C.', 'Frutas y verduras': 'Seleccionada cada mañana desde productores locales. Lavar antes de consumir.', Despensa: 'Producto de despensa de calidad, envasado en origen. Conservar en lugar fresco y seco.', Panadería: 'Horneado a diario en nuestras tiendas. Ideal para consumir el mismo día.', Bebidas: 'Bebida lista para consumir. Servir bien fría.', Limpieza: 'Fórmula de alto rendimiento para el hogar. Mantener fuera del alcance de los niños.', 'Carnes y pescados': 'Corte seleccionado y envasado al vacío. Mantener en frío hasta su preparación.', Congelados: 'Mantener a −18 °C. No volver a congelar una vez descongelado.' };
function renderPdp(p) {
  const rel = [...PRODUCTS.filter((x) => x.cat === p.cat && x.id !== p.id), ...PRODUCTS.filter((x) => x.cat !== p.cat)].slice(0, 6);
  const tint = `--bg:${TINT[p.cat][0]};color:${TINT[p.cat][1]}`;
  $('#pdp').innerHTML = `<nav class="crumbs" aria-label="Ruta"><a href="#/">Inicio</a>${icon('chevron-right')}<a href="#" data-cat="${p.cat}">${p.cat}</a>${icon('chevron-right')}<span>${esc(p.name)}</span></nav>
    <div class="pdp-grid"><div class="gal"><div class="main-img" style="${tint}">${pic(p)}</div>
      <div class="thumbs">${[0, 1, 2].map((i) => `<span class="th ${i === 0 ? 'on' : ''}" style="${tint}">${pic(p)}</span>`).join('')}</div></div>
    <div class="buy" data-card="${p.id}"><div class="brand">${esc(p.brand)}</div><h1>${esc(p.name)}</h1>
      <div class="rate">${icon('star', '', 1.5)}<b>${p.rate}</b><span>· ${20 + Number(p.id.slice(1)) * 7} opiniones</span></div>
      <div class="price-lg">${peso(p.price)}${p.old ? `<span class="old">${peso(p.old)}</span><span class="save">Ahorras ${peso(p.old - p.price)}</span>` : ''}</div>
      <div class="act">${act(p)}</div>
      <ul class="perks"><li>${icon('truck')}<span><b>Despacho el mismo día</b>En pedidos antes de las 14:00</span></li><li>${icon('store')}<span><b>Retiro en tienda</b>Gratis en 2 horas</span></li><li>${icon('rotate-ccw')}<span><b>Cambios sin costo</b>Dentro de 48 horas</span></li></ul></div></div>
    <h2 class="sec">Descripción</h2><p class="desc">${DESC[p.cat]}</p>
    ${rel.length ? `<div class="shelf-head"><h2>Productos relacionados</h2></div><div class="shelf-wrap">${ARROWS}<div class="row">${rel.map(card).join('')}</div></div>` : ''}`;
}
/** La ficha también sale del catálogo: esqueleto mientras responde y error si no. */
async function showProduct(p) {
  const key = viewKey();
  skeleton($('#pdp'), 4);
  document.title = p.name + ' · Mercado Don Federico';
  const r = await userAction(PLATFORM, 'browse');
  if (viewKey() !== key) return;
  if (!r.ok) {
    errorState($('#pdp'), { what: 'No pudimos cargar el producto', result: r, retry: () => showProduct(p) });
    document.title = 'Producto no disponible · Mercado Don Federico';
    return;
  }
  renderPdp(p);
}
function route() {
  const m = location.hash.match(/^#\/producto\/(p\d+)/), p = m && PRODUCTS.find((x) => x.id === m[1]);
  HOME.forEach((s) => { $(s).hidden = Boolean(p); });
  $('#pdp').hidden = !p;
  if (p) showProduct(p);
  else { document.title = 'Mercado Don Federico · Tu supermercado en línea'; showShelves(); }
  if (m || state.routed) scrollTo(0, 0);
  state.routed = true;
}
addEventListener('hashchange', route);
route();
renderCart();
/* Entrada escalonada de la portada (una sola vez). */
['.page > .hero', '.club-banner', '#shelves .shelf-head:first-child', '#shelves .shelf-wrap:first-of-type'].forEach((s, i) => { const el = $(s); if (el) { el.classList.add('rise'); el.style.setProperty('--i', i); } });
