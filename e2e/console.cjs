// Prueba de extremo a extremo de la consola del Demo Day.
// Requiere demo-lab corriendo en http://localhost:7000 y Playwright con
// Chromium. Las fallas necesitan el escenario preparado (token de ingesta); si
// no lo está, esa parte se omite y el resto de la consola se prueba igual.
// Uso: node e2e/console.cjs
const path = require('path');
let chromium;
try { ({ chromium } = require('playwright-core')); } catch { ({ chromium } = require(path.join(__dirname, '../../client/node_modules/playwright-core'))); }

const BASE = process.env.DEMO_LAB_URL || 'http://localhost:7000';
// Con A6 activa (DEMO_CONSOLE_KEY definida en demo-lab), la prueba manda la
// clave de la consola en cada petición, de la API y del navegador.
const AUTH = process.env.DEMO_CONSOLE_KEY ? { Authorization: 'Bearer ' + process.env.DEMO_CONSOLE_KEY } : {};
let failed = 0;
const ok = (cond, name) => { console.log((cond ? '  ok   ' : '  FALLA ') + name); if (!cond) failed++; };
const seen = (loc, ms = 8000) => loc.first().waitFor({ state: 'visible', timeout: ms }).then(() => true, () => false);

const ctl = async (method, p, body) => {
  const r = await fetch(BASE + '/control/v1' + p, {
    method,
    headers: { ...AUTH, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: r.status, body: r.status === 204 ? null : await r.json().catch(() => null) };
};

(async () => {
  const browser = await chromium.launch();
  const errors = [];
  const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 }, extraHTTPHeaders: AUTH })).newPage();
  page.on('pageerror', (e) => errors.push(e.message));

  const initial = (await ctl('GET', '/platforms')).body || [];
  const initialTraffic = Object.fromEntries(initial.map((p) => [p.platform, Number(p.traffic_rps || 0)]));

  console.log('Pestañas y estructura');
  await page.goto(BASE + '/');
  ok(await seen(page.locator('#tab-inicio')), 'abre en Inicio');
  for (const tab of ['mercado', 'banco', 'clinica', 'actividad', 'datos']) {
    ok(await page.locator(`#tab-${tab}`).isVisible(), `la pestaña ${tab} existe`);
  }
  await page.click('#tab-banco');
  ok((await page.evaluate(() => location.hash)) === '#banco', 'la pestaña activa queda en la URL (#banco)');
  ok(await seen(page.locator('[data-service="banco/api-transferencias"]')), 'la pestaña Banco arma sus servicios');

  console.log('Redibujo sin reemplazar el DOM');
  const traffic = page.locator('[data-traffic="banco"]');
  await traffic.fill('2');
  await page.locator('[data-service="banco/api-transferencias"] .service-toggle').click();
  ok(await page.locator('[data-service="banco/api-transferencias"] .service-detail').isVisible(), 'la fila del servicio se despliega');
  await page.waitForTimeout(3600);
  ok((await traffic.inputValue()) === '2', 'el valor escrito en tráfico sobrevive a varios sondeos');
  ok(await page.locator('[data-service="banco/api-transferencias"] .service-detail').isVisible(), 'la fila abierta sobrevive a varios sondeos');

  console.log('Guía');
  const guide = await page.evaluate(async () => (await import('/console/guide.js')).GUIDE);
  const refs = await page.evaluate(() => [...document.querySelectorAll('[data-guide]')].map((el) => el.dataset.guide));
  ok(refs.length > 0 && refs.every((id) => guide[id]), 'cada data-guide tiene una entrada en GUIDE');
  const scripts = (await ctl('GET', '/scripts')).body || {};
  ok((scripts.scripts || []).every((s) => guide['script-' + s.id]), 'cada guion incluido tiene explicación');
  const attacks = (await ctl('GET', '/attacks')).body || {};
  ok((attacks.kinds || []).every((k) => guide['attack-' + k.name]), 'cada tipo de ataque tiene explicación');

  for (const tab of ['inicio', 'mercado', 'banco', 'clinica', 'actividad', 'datos']) {
    await page.click(`#tab-${tab}`);
    await page.click('#btn-guide');
    ok(await page.locator('#guide').isVisible(), `la guía abre desde ${tab}`);
    await page.keyboard.press('Escape');
    ok(!(await page.locator('#guide').isVisible()), `Esc cierra la guía en ${tab}`);
  }

  console.log('Falla desde la consola');
  await page.click('#tab-banco');
  const detail = page.locator('[data-service="banco/api-transferencias"] .service-detail');
  if (!(await detail.isVisible())) await page.locator('[data-service="banco/api-transferencias"] .service-toggle').click();
  const probe = await ctl('POST', '/faults', {
    platform: 'banco', service: 'api-transferencias', type: 'down', params: { mode: '503' },
  });
  if (probe.status === 201) {
    await ctl('DELETE', '/faults/' + probe.body.id);
    await page.locator('[data-fault="banco/api-transferencias/fault-503"]').click();
    ok(await seen(page.locator('[data-fault-chip]').first()), 'la falla aplicada aparece en la fila del servicio');
    const live = (await ctl('GET', '/faults')).body || [];
    ok(live.some((f) => f.service === 'api-transferencias' && f.type === 'down'), 'el botón «Responder 503» aplica la falla');
    const count = page.locator('[data-tab-count="banco"]');
    ok(!(await count.getAttribute('hidden')) && (await count.innerText()) === '1', 'la pestaña Banco muestra el contador de fallas');
    ok((await page.locator('[data-tab-dot="banco"]').getAttribute('class')).includes('caido'), 'el punto de la pestaña Banco pasa a caído');
    await page.locator('[data-fault-chip] .chip-x').first().click();
    await page.waitForTimeout(900);
    ok(((await ctl('GET', '/faults')).body || []).length === 0, 'la ✕ retira la falla');
    ok((await page.locator('[data-tab-dot="banco"]').getAttribute('class')).includes('ok'), 'el punto de la pestaña Banco vuelve a sano');
  } else {
    console.log('  omite  falla desde la consola: el escenario no está preparado (' + probe.status + ')');
  }

  console.log('Confirmación del reinicio');
  await page.click('#btn-menu');
  await page.click('#btn-reset');
  const armed = await page.locator('#btn-reset').innerText();
  ok(armed.includes('¿Seguro?'), 'el primer toque pide confirmación');
  await page.click('#btn-reset');
  ok(await seen(page.locator('#toasts .toast').filter({ hasText: 'Reiniciado' }), 15000), 'el segundo toque reinicia el escenario');
  ok((await page.locator('#btn-reset').innerText()) === 'Reiniciar escenario', 'el botón vuelve a su texto normal');

  // Dejar el tráfico como estaba antes de la prueba.
  for (const [platform, rps] of Object.entries(initialTraffic)) {
    await ctl('PUT', '/traffic/' + platform, { rps });
  }
  await ctl('POST', '/platforms/mercado/restore');
  await ctl('POST', '/platforms/banco/restore');
  await ctl('POST', '/platforms/clinica/restore');

  console.log('Tablet vertical');
  const tabletContext = await browser.newContext({ viewport: { width: 820, height: 1180 }, hasTouch: true, isMobile: true, extraHTTPHeaders: AUTH });
  const tablet = await tabletContext.newPage();
  tablet.on('pageerror', (e) => errors.push('tablet: ' + e.message));
  await tablet.goto(BASE + '/');
  await tablet.click('#tab-banco');
  ok(await seen(tablet.locator('[data-service="banco/api-transferencias"] .service-toggle')), 'la consola se arma en tablet vertical');
  ok(await tablet.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'sin scroll horizontal en tablet vertical');
  await tablet.click('#btn-guide');
  const guideBox = await tablet.locator('#guide').boundingBox();
  ok(guideBox && guideBox.width >= 800, 'la guía ocupa todo el ancho en tablet');
  await tabletContext.close();

  ok(errors.length === 0, 'sin errores de JavaScript en la consola' + (errors.length ? ': ' + errors.join(' | ') : ''));
  await browser.close();
  console.log(failed ? `\n${failed} verificaciones fallaron` : '\ntodo en orden');
  process.exit(failed ? 1 : 0);
})();
