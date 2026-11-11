import { $, $$, esc, userAction, toast, carousel, boot, skeleton, errorState, errorText } from '../shared/common.js';
import { icon, sceneIcon, foto } from '../shared/icons.js';

const PLATFORM = 'clinica';
const DEMO = { rut: '11.111.111-1', pass: 'demo1234' };
const SPECIALTIES = {
  'Medicina general': ['Dra. Paula Fuentes', 'Dr. Mario Cabrera'], Cardiología: ['Dr. Ignacio Soto'], Dermatología: ['Dra. Marcela Rivas'],
  Pediatría: ['Dr. Andrés Vera', 'Dra. Lorena Mena'], Ginecología: ['Dra. Isidora Prado'], Traumatología: ['Dr. Felipe Araya'],
};

/* Íconos de Lucide (ver shared/icons.js). */
const NAMES = { flask: 'flask-conical', doc: 'file-text', doctor: 'stethoscope', invoice: 'receipt', cal: 'calendar-days', laptop: 'laptop', pulse: 'heart-pulse', heart: 'heart', shield: 'shield-check', arrow: 'arrow-right' };
const ic = (n) => icon(NAMES[n] || n);
const QUICK = [['cal', 'Reservar hora', 'Consulta o control'], ['flask', 'Exámenes', 'Pide tu hora'], ['doctor', 'Médicos', 'Busca por nombre'], ['invoice', 'Presupuestos', 'Cotiza en línea'], ['cal', 'Mis horas', 'Revisa o cambia'], ['laptop', 'Salud digital', 'Telemedicina'], ['shield', 'Seguros', 'Cotiza tu plan']];
const SERVICES = {
  Todos: [['Exámenes genéticos del cáncer', 'Estudios que integran tecnología avanzada para tu prevención.'], ['Urgencia gineco-obstétrica', 'Atención de emergencia las 24 horas, todos los días.'], ['Cirugía robótica', 'Procedimientos mínimamente invasivos con apoyo robótico.'], ['Medicina nuclear', 'Diagnóstico y tratamiento con técnicas de imagen avanzada.'], ['Anatomía patológica', 'Análisis de tejidos para diagnósticos precisos.'], ['Cirugía mayor ambulatoria', 'Vuelve a casa el mismo día tras tu procedimiento.'], ['Salud de la mujer', 'Controles, ecografías y acompañamiento en cada etapa.'], ['Rehabilitación', 'Kinesiología y terapias para recuperar tu movilidad.']],
  Prevención: [['Chequeo preventivo', 'Evaluación completa según tu edad y antecedentes.'], ['Mamografía', 'Imagen digital con lectura de especialista.'], ['Vacunación', 'Esquemas para niños y adultos.'], ['Control del niño sano', 'Seguimiento del crecimiento y desarrollo.']],
  Urgencias: [['Urgencia adulto', 'Médicos de turno todo el año.'], ['Urgencia pediátrica', 'Atención especializada para niños.'], ['Traslado', 'Ambulancias equipadas.'], ['Orientación 24 horas', 'Un profesional te guía por teléfono.']],
};
const SVC_ICONS = ['pulse', 'heart', 'flask', 'shield', 'cal', 'doctor'];
const CENTERS = [['Clínica Santiago', 'Av. Principal 1234, Santiago'], ['Clínica Las Condes', 'Camino Los Robles 5678, Las Condes'], ['Centro médico Maipú', 'Av. Del Sol 910, Maipú'], ['Clínica Valparaíso', 'Calle Mirador 321, Valparaíso']];
const NEWS = [['#dcdcfb,#c9f0e6', 'Pediatría', '¿Cómo ayudar a tu hijo si le diagnostican diabetes?', 'Guía para familias en los primeros días.'], ['#fbd6dc,#fde9c7', 'Dermatología', 'Mastocitosis: qué es y cuándo consultar', 'Síntomas, diagnóstico y tratamiento.'], ['#c9f0e6,#dcdcfb', 'Salud mental', 'El bajón emocional después de un gran evento', 'Recomendaciones de especialistas.']];
const PLANS = [['Plan familiar', '$15.281', 'Cobertura ambulatoria para toda la familia.'], ['Seguro complementario', '$11.260', 'Respaldo adicional en hospitalizaciones.'], ['Plan dental', '$9.249', 'Atención preventiva y tratamientos.']];
const EXAMS = [['18/10', 'Hemograma completo'], ['18/10', 'Perfil lipídico'], ['02/09', 'Radiografía de tórax']];
const MEGA = [
  [['Especialidades médicas', ['Cardiología', 'Ginecología y obstetricia', 'Neurología', 'Oncología', 'Traumatología', 'Ver todas']], ['Centros', ['Centro cardiovascular', 'Centro de salud mayor', 'Instituto oncológico']], ['Programas', ['Control de peso', 'Salud integral de la mujer', 'Unidad del adulto mayor']]],
  [['Ambulatorios', ['Banco de sangre', 'Kinesiología', 'Medicina general', 'Telemedicina', 'Vacunatorio']], ['Exámenes y procedimientos', ['Imagenología', 'Laboratorios', 'Procedimientos', 'Toma de muestras a domicilio']], ['Hospitalarios', ['Cirugías', 'Hospitalización', 'Maternidad', 'Urgencia']]],
  [['Clínicas', ['Santiago', 'Las Condes', 'Valparaíso']], ['Centros médicos', ['Maipú', 'Providencia', 'Ñuñoa']], ['Horarios y contacto', ['Centros y horarios', 'Urgencias', 'Contacto']]],
];

/* Fotografías de licencia libre (ver shared/photos/creditos.html); el teléfono del bloque de la app es un ícono. */
const TELEFONO = `<svg viewBox="0 0 280 460" aria-hidden="true">${sceneIcon('smartphone', 20, 40, 240, '#bfefe6', 1)}${sceneIcon('heart-pulse', 90, 160, 100, '#fff', 1.5)}</svg>`;
$$('[data-art]').forEach((el) => { el.innerHTML = el.dataset.art === 'telefono' ? TELEFONO : foto('c-hero-' + el.dataset.art, '', 'hero', el.dataset.art === 'cinta'); });
const building = (i) => foto('c-centro-' + (i + 1), CENTERS[i][0]);
const newsArt = (g, i) => foto('c-news-' + (i + 1), NEWS[i][2]);

boot(PLATFORM);
carousel($('#hero'), 7000);
/* Contador del paginador «1/3» a partir de la diapositiva activa. */
const slides = $$('#hero .slide');
const syncPager = () => { $('#pg').textContent = `${Math.max(1, slides.findIndex((s) => s.classList.contains('on')) + 1)}/${slides.length}`; };
slides.forEach((s) => new MutationObserver(syncPager).observe(s, { attributes: true, attributeFilter: ['class'] }));
syncPager();

/* ── Cabecera: menús y aviso ──────────────────────────────────────────── */
const mega = $('#mega'), navBtns = $$('[data-mega]');
const closeMega = () => { mega.classList.remove('open'); navBtns.forEach((b) => b.setAttribute('aria-expanded', 'false')); };
navBtns.forEach((b) => b.addEventListener('click', (e) => {
  e.stopPropagation();
  const open = b.getAttribute('aria-expanded') === 'true';
  closeMega(); if (open) return;
  $('#mega-box').innerHTML = MEGA[Number(b.dataset.mega)].map(([t, ls]) => `<div><h5>${t}</h5>${ls.map((l) => `<a href="${Number(b.dataset.mega) === 0 ? '#/profesionales' : '#reserva'}">${l}</a>`).join('')}</div>`).join('');
  b.setAttribute('aria-expanded', 'true'); mega.classList.add('open');
}));
mega.addEventListener('click', closeMega);
document.addEventListener('click', closeMega);
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeMega(); });
$('#notice-x').onclick = () => { const n = $('#notice'); n.classList.add('gone'); setTimeout(() => { n.hidden = true; $('.hero').style.paddingTop = '42px'; }, 260); };

$('#b-spec').innerHTML = Object.keys(SPECIALTIES).map((s) => `<option>${s}</option>`).join('');
$('#quick').innerHTML = QUICK.map(([a, t, s], i) => `<div class="qcard ${i === 0 ? 'first' : ''}" data-q="${i}" role="button" tabindex="0"><span class="ic">${ic(a)}</span><b>${t}</b><small>${s}</small></div>`).join('');
$('#centers').innerHTML = CENTERS.map(([n, d], i) => `<div class="ccard"><div class="img">${building(i)}</div><div class="txt"><b>${n}</b><small>${d}</small></div></div>`).join('');
$('#news').innerHTML = NEWS.map(([g, c, t, d], i) => `<div class="ncard"><div class="img">${newsArt(g, i)}</div><div class="txt"><small>${c}</small><h3>${t}</h3><p>${d}</p></div></div>`).join('');
$('#plans').innerHTML = PLANS.map(([n, p, d]) => `<div class="plan"><h3>${n}</h3><span class="from">Desde, al mes</span><span class="price">${p}*</span><p>${d}</p><a class="btn" href="#reserva">Ver más</a></div>`).join('');
function renderServices(tab) {
  $('#chips').innerHTML = Object.keys(SERVICES).map((k) => `<button class="chip ${k === tab ? 'on' : ''}" data-tab="${k}">${k}</button>`).join('');
  $('#svc').innerHTML = SERVICES[tab].map(([t, d], i) => `<div class="scard" style="--i:${i}"><span class="ic">${ic(SVC_ICONS[i % 6])}</span><h3>${t}</h3><p>${d}</p><span class="go">${ic('arrow')}</span></div>`).join('');
}
renderServices('Todos');
document.addEventListener('click', (e) => { const t = e.target.closest('[data-tab]'); if (t) renderServices(t.dataset.tab); });

/* ── Modal sencillo ───────────────────────────────────────────────────── */
function modal(html) {
  const back = document.createElement('div'); back.className = 'modal-back';
  back.innerHTML = `<div class="modal">${html}</div>`;
  back.addEventListener('click', (e) => { if (e.target === back) back.remove(); });
  $('#modal-root').append(back);
  return back;
}

/* ── Sesión «Mi Clínica» (solo credenciales demo) ─────────────────────── */
let session = false;
function login() {
  return new Promise((resolve) => {
    if (session) return resolve(true);
    const m = modal(`<h2>Ingresa a Mi Clínica</h2><p class="hint">Demostración: RUT <b>${DEMO.rut}</b>, clave <b>${DEMO.pass}</b>. No ingreses datos reales.</p>
      <form><label class="fld">RUT<input name="rut"></label><label class="fld">Clave<input name="pass" type="password"></label><div id="lm"></div><button class="mbtn" style="width:100%">Ingresar</button></form>`);
    m.addEventListener('click', (e) => { if (e.target === m) resolve(false); });
    m.querySelector('input').focus();
    m.querySelector('form').onsubmit = async (e) => {
      e.preventDefault();
      const f = e.target, lm = m.querySelector('#lm');
      if (f.rut.value.trim() !== DEMO.rut || f.pass.value !== DEMO.pass) { lm.innerHTML = '<div class="err-box">Solo se aceptan las credenciales de demostración.</div>'; return; }
      const r = await userAction(PLATFORM, 'login');
      if (r.ok) { session = true; $('#mi-label').textContent = 'Hola, Paciente'; m.remove(); resolve(true); }
      else lm.innerHTML = `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos iniciar tu sesión'))}</strong>Intenta nuevamente en unos minutos.</div>`;
    };
  });
}

/* ── Reserva de hora ──────────────────────────────────────────────────── */
$('#book-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const spec = $('#b-spec').value, center = $('#b-center').value, btn = $('#b-go');
  btn.disabled = true; btn.textContent = 'Buscando horas…';
  const r = await userAction(PLATFORM, 'search');
  btn.disabled = false; btn.textContent = 'Buscar horas';
  if (!r.ok) { modal(`<h2>Buscar horas</h2><div class="err-box"><strong>${esc(errorText(r, 'No pudimos buscar horas disponibles'))}</strong>Intenta nuevamente en unos minutos.</div>`); return; }
  const docs = SPECIALTIES[spec], base = [['mañana', '09:00'], ['mañana', '10:30'], ['jueves', '12:15'], ['viernes', '16:40']];
  const m = modal(`<h2>Horas de ${esc(spec)}</h2><p class="hint">${esc(center)} · ${esc($('#b-type').value)}</p>
    <div class="slots">${base.map(([d, h], i) => `<div class="slot"><div><b>${d} · ${h}</b><small>${esc(docs[i % docs.length])}</small></div><button data-i="${i}">Reservar</button></div>`).join('')}</div><div id="bm" aria-live="polite"></div>`);
  m.addEventListener('click', async (ev) => {
    const b = ev.target.closest('button[data-i]'); if (!b) return;
    const [d, h] = base[b.dataset.i], doc = docs[b.dataset.i % docs.length];
    if (!(await login())) return;
    b.disabled = true; b.textContent = 'Reservando…';
    const res = await userAction(PLATFORM, 'book_appointment');
    b.disabled = false; b.textContent = 'Reservar';
    $('#bm', m).innerHTML = res.ok
      ? `<div class="ok-box"><strong>Hora confirmada</strong>${esc(doc)} · ${d} a las ${h} en ${esc(center)}. Comprobante N° ${esc(res.receipt)}</div>`
      : `<div class="err-box"><strong>${esc(errorText(res, 'No pudimos confirmar tu hora'))}</strong>Tu hora no fue reservada.</div>`;
  });
});

/* ── Mi Clínica: exámenes ─────────────────────────────────────────────── */
async function openMine() {
  if (!(await login())) return;
  const m = modal(`<h2>Mis exámenes de laboratorio</h2><div id="ex-list"></div><div id="em" aria-live="polite"></div>`);
  const loadExams = async () => {
    const list = $('#ex-list', m);
    if (!list) return;
    skeleton(list, 3);
    const r = await userAction(PLATFORM, 'list_exams');
    if (!r.ok) {
      errorState(list, { what: 'No pudimos cargar tus exámenes', result: r, retry: loadExams });
      return;
    }
    list.innerHTML = `<ul class="exams">${EXAMS.map(([f, n], i) => `<li><span><b>${esc(n)}</b><br><small style="color:#5c5c5c">${f}</small></span><button class="mbtn ghost" data-ex="${i}">Descargar PDF</button></li>`).join('')}</ul>`;
  };
  loadExams();
  m.addEventListener('click', async (ev) => {
    const b = ev.target.closest('[data-ex]'); if (!b) return;
    b.disabled = true; b.textContent = 'Descargando…';
    const r = await userAction(PLATFORM, 'download_exam');
    b.disabled = false; b.textContent = 'Descargar PDF';
    $('#em', m).innerHTML = r.ok
      ? `<div class="ok-box"><strong>Examen descargado</strong>${esc(EXAMS[b.dataset.ex][1])}. Comprobante N° ${esc(r.receipt)}</div>`
      : `<div class="err-box"><strong>${esc(errorText(r, 'No pudimos descargar tu examen'))}</strong>Intenta nuevamente en unos minutos.</div>`;
  });
}
$('#btn-mi').onclick = openMine;
$('#link-mi').onclick = (e) => { e.preventDefault(); openMine(); };
$('#app-reg').onclick = (e) => { e.preventDefault(); toast('Registro no disponible en la demostración'); };
$('#gsearch').addEventListener('submit', (e) => { e.preventDefault(); toast('Búsqueda general no disponible en la demostración'); });
const quick = (c) => { if (c.dataset.q === '4') openMine(); else if (c.dataset.q === '2') location.hash = '#/profesionales'; else $('#reserva').scrollIntoView({ behavior: 'smooth' }); };
$$('.qcard').forEach((c) => { c.addEventListener('click', () => quick(c)); c.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); quick(c); } }); });
/* ── Buscador de profesionales (ruta #/profesionales) ─────────────────── */
const CENTER_LIST = ['Santiago Centro', 'Las Condes', 'Maipú', 'Valparaíso'];
const NEXT = ['Hoy 16:40', 'Mañana 09:00', 'Jueves 12:15', 'Viernes 10:30'];
const PROS = Object.entries(SPECIALTIES).flatMap(([spec, names], si) => names.map((name, i) => ({
  name, spec, centers: [CENTER_LIST[(si + i) % 4], CENTER_LIST[(si + i + 1) % 4]], next: NEXT[(si + i) % 4],
  rate: (4.3 + ((si * 3 + i * 2) % 7) / 10).toFixed(1), years: 6 + ((si * 5 + i * 3) % 16),
})));
function proCard(p, i) {
  return `<article class="pro" style="--i:${i}"><span class="av">${icon('user-round', '', 1.4)}</span>
    <div class="pi"><h2>${esc(p.name)}</h2><span class="sp">${esc(p.spec)}</span>
      <ul><li>${icon('map-pin')}${p.centers.map(esc).join(' · ')}</li><li>${icon('star')}${p.rate} · ${p.years} años de experiencia</li><li>${icon('calendar-days')}Próxima hora: <b>${p.next}</b></li></ul></div>
    <button class="cta sm" data-pro="${PROS.indexOf(p)}">Reservar hora</button></article>`;
}
let prosOK = false;
function renderPros() {
  if (!prosOK) return;
  const q = ($('#p-q').value || '').trim().toLowerCase(), sp = $('#p-spec').value, ce = $('#p-center').value;
  const list = PROS.filter((p) => (!q || (p.name + ' ' + p.spec).toLowerCase().includes(q)) && (!sp || p.spec === sp) && (!ce || p.centers.includes(ce)));
  $('#p-count').textContent = `${list.length} ${list.length === 1 ? 'profesional' : 'profesionales'}`;
  $('#p-list').innerHTML = list.length ? list.map(proCard).join('') : '<div class="empty">No encontramos profesionales con ese criterio.</div>';
}
async function loadPros() {
  const list = $('#p-list');
  if (!list) return;
  skeleton(list, 4);
  const r = await userAction(PLATFORM, 'search');
  if (!r.ok) {
    prosOK = false;
    $('#p-count').textContent = '';
    errorState(list, { what: 'No pudimos cargar la disponibilidad de profesionales', result: r, retry: loadPros });
    return;
  }
  prosOK = true;
  renderPros();
}
function mountPros() {
  $('#pros').innerHTML = `<nav class="crumbs" aria-label="Ruta"><a href="#/">Inicio</a>${icon('chevron-right')}<span>Buscador de profesionales</span></nav>
    <h1 class="sec left">Encuentra a tu <b>especialista</b></h1>
    <form class="pfilter" id="p-form" role="search"><label>Nombre o especialidad<input id="p-q" placeholder="Ej. Cardiología" autocomplete="off"></label>
      <label>Especialidad<select id="p-spec"><option value="">Todas</option>${Object.keys(SPECIALTIES).map((s) => `<option>${s}</option>`).join('')}</select></label>
      <label>Centro<select id="p-center"><option value="">Todos</option>${CENTER_LIST.map((c) => `<option>${c}</option>`).join('')}</select></label></form>
    <p class="pcount" id="p-count" aria-live="polite"></p><div class="plist" id="p-list"></div>`;
  ['#p-q', '#p-spec', '#p-center'].forEach((s) => $(s).addEventListener('input', renderPros));
  $('#p-form').addEventListener('submit', (e) => e.preventDefault());
  $('#p-list').addEventListener('click', (e) => {
    const b = e.target.closest('[data-pro]'); if (!b) return;
    const p = PROS[Number(b.dataset.pro)];
    $('#b-spec').value = p.spec; $('#b-center').value = $('#p-center').value || p.centers[0];
    $('#book-form').requestSubmit();
  });
  loadPros();
}
let prosMounted = false;
function route() {
  const pros = location.hash === '#/profesionales';
  $('#home').hidden = pros; $('#pros').hidden = !pros;
  if (pros) { if (!prosMounted) { mountPros(); prosMounted = true; } document.title = 'Buscador de profesionales · Clínica Don Federico'; scrollTo(0, 0); }
  else { document.title = 'Clínica Don Federico · Salud para tu familia'; const a = location.hash.length > 1 && !location.hash.startsWith('#/') ? document.querySelector(location.hash) : null; if (a) a.scrollIntoView(); }
}
addEventListener('hashchange', route);
route();
/* Entrada escalonada de la portada (una sola vez). */
['.notice', '.hero', '.quick', '.booking'].forEach((s, i) => { const el = $(s); if (el) { el.classList.add('rise'); el.style.setProperty('--i', i); } });
