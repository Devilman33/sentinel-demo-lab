// Prueba de extremo a extremo de los sitios ficticios (TC-06, 07, 08, 14, 20, 22).
// Requiere demo-lab corriendo en http://localhost:7000 con el escenario
// preparado (las fallas exigen que Sentinel reciba la ingesta) y Playwright con
// Chromium. Uso:  node e2e/sites.cjs
const path = require('path');
let chromium;
try { ({ chromium } = require('playwright-core')); } catch { ({ chromium } = require(path.join(__dirname, '../../client/node_modules/playwright-core'))); }

const BASE = process.env.DEMO_LAB_URL || 'http://localhost:7000';
// Con A6 activa (DEMO_CONSOLE_KEY definida en demo-lab), la prueba manda la
// clave de la consola en cada petición, de la API y del navegador.
const AUTH = process.env.DEMO_CONSOLE_KEY ? { Authorization: 'Bearer ' + process.env.DEMO_CONSOLE_KEY } : {};
let failed = 0, total = 0;
const ok = (cond, name) => { total++; console.log((cond ? '  ok   ' : '  FALLA ') + name); if (!cond) failed++; };

// locator.isVisible() no espera: se espera explícitamente a que aparezca.
const seen = (loc, ms = 8000) => loc.first().waitFor({ state: 'visible', timeout: ms }).then(() => true, () => false);

const ctl = async (method, p, body) => {
  const r = await fetch(BASE + '/control/v1' + p, { method, headers: AUTH, body: body ? JSON.stringify(body) : undefined });
  return r.status === 204 ? null : r.json().catch(() => null);
};
const fault = (platform, service, mode = 'refused') => ctl('POST', '/faults', { platform, service, type: 'down', params: { mode } });
const faultKind = (platform, service, type, params) => ctl('POST', '/faults', { platform, service, type, params });
const restore = (p) => ctl('POST', `/platforms/${p}/restore`);
const BFF = { mercado: 'storefront-web', banco: 'banca-en-linea', clinica: 'portal-paciente' };

(async () => {
  const browser = await chromium.launch();
  const errors = [];
  const newPage = async () => {
    const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 }, extraHTTPHeaders: AUTH })).newPage();
    page.on('pageerror', (e) => errors.push(e.message));
    return page;
  };
  for (const p of ['mercado', 'banco', 'clinica']) await restore(p);

  console.log('Mercado');
  let page = await newPage();
  await page.goto(BASE + '/sitios/mercado/');
  ok(await seen(page.locator('.fiction-strip')), 'muestra la franja de sitio ficticio (TC-20)');
  ok(await seen(page.locator('[data-inc="p1"]')), 'el catálogo carga por browse antes de dibujar');
  await page.locator('[data-inc="p1"]').first().click();
  await page.locator('[data-inc="p3"]').first().click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '2');
  await page.click('#btn-cart');
  await page.click('#pay');
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Pago aprobado' })), 'con servicios sanos el pago se aprueba y da comprobante (TC-06)');

  await fault('mercado', 'db-transaccional');
  await page.locator('#drawer-close').click();
  await page.locator('[data-inc="p2"]').first().click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '1');
  await page.click('#btn-cart');
  await page.click('#pay');
  const err = page.locator('.err-box');
  ok(await seen(err.filter({ hasText: 'No pudimos procesar tu pago' })) && (await err.innerText()).includes('Error 503'), 'con la BD caída el pago falla con error 503 (TC-07/08)');
  ok((await err.innerText()).includes('db-transaccional'), 'el error nombra el servicio causante');
  await restore('mercado');
  await page.click('#pay');
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Pago aprobado' })), 'tras restaurar el pago vuelve a funcionar (TC-14)');
  await page.locator('#drawer-close').click();

  // El catálogo caído no borra el carro ya armado: agregar avisa y pagar sigue.
  await page.locator('[data-inc="p2"]').first().click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '1');
  const countBefore = await page.innerText('#cart-count');
  await fault('mercado', 'api-catalogo');
  await page.locator('[data-inc="p2"]').first().click();
  ok(await seen(page.locator('.toast').filter({ hasText: 'No pudimos agregar el producto' })), 'con el catálogo caído «Agregar» avisa el error');
  ok((await page.innerText('#cart-count')) === countBefore, 'y el carro no cambia');
  await page.click('#btn-cart');
  await page.click('#pay');
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Pago aprobado' })), 'con el catálogo caído el carro ya armado se paga (D-16)');
  await page.locator('#drawer-close').click();

  // Con la página ya abierta, cada vista vuelve a pedir el catálogo al mostrarse.
  await page.locator('#tiles [data-cat="Lácteos"]').click();
  ok(await seen(page.locator('#shelves .state-error').filter({ hasText: 'No pudimos cargar la categoría Lácteos' })), 'con el catálogo caído una categoría muestra el error');
  ok((await page.locator('#shelves .card .add').count()) === 0, 'y no dibuja productos guardados');
  await page.goto(BASE + '/sitios/mercado/#/producto/p3');
  ok(await seen(page.locator('#pdp .state-error').filter({ hasText: 'No pudimos cargar el producto' })), 'con el catálogo caído la ficha de producto muestra el error');

  await page.fill('#q', 'leche');
  await page.press('#q', 'Enter');
  ok(await seen(page.locator('#shelves .state-error').filter({ hasText: 'No pudimos buscar productos' })), 'con el catálogo caído la búsqueda muestra el error en el estante');
  await page.reload();
  ok(await seen(page.locator('#shelves .state-error')), 'al cargar con el catálogo caído los estantes no se dibujan');
  ok((await page.locator('.card .add').count()) === 0, 'y no hay ningún botón «Agregar»');
  await restore('mercado');
  await page.close();

  console.log('Banco');
  page = await newPage();
  await page.goto(BASE + '/sitios/banco/');
  await page.fill('input[name=rut]', '99.999.999-9'); await page.fill('input[name=pass]', 'x');
  await page.click('#login button');
  ok(await seen(page.locator('.err-box').filter({ hasText: 'credenciales de demostración' })), 'rechaza credenciales que no son de demostración (TC-20)');
  await fault('banco', 'auth-banca');
  await page.fill('input[name=rut]', '11.111.111-1'); await page.fill('input[name=pass]', 'demo1234');
  await page.click('#login button');
  ok(await seen(page.locator('.err-box').filter({ hasText: 'No pudimos validar tu clave' })), 'con auth-banca caído no se puede iniciar sesión');
  await restore('banco');
  await page.click('#login button');
  ok(await seen(page.locator('.nav')), 'con servicios sanos inicia sesión y ve el portal');
  ok((await page.innerText('.page')).includes('Accesos directos'), 'el home muestra accesos directos y cuentas');

  const transfer = async () => {
    await page.goto(BASE + '/sitios/banco/#/home');
    await page.goto(BASE + '/sitios/banco/#/transferir');
    await page.selectOption('#dest', { index: 1 });
    await page.fill('#amount', '50000');
    await page.click('#next'); await page.click('#next');
    const d = page.locator('.otp-d');
    for (let i = 0; i < 6; i++) await d.nth(i).fill(String(i + 1));
    await page.click('#send');
  };
  await transfer();
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Transferencia realizada' })), 'transferencia exitosa con comprobante (TC-06)');
  await fault('banco', 'core-bancario');
  await page.goto(BASE + '/sitios/banco/#/home');
  ok(await seen(page.locator('#bal-box .state-error').filter({ hasText: 'No pudimos cargar tu saldo disponible' })), 'con core-bancario caído el home no muestra el saldo');
  await page.goto(BASE + '/sitios/banco/#/transferir');
  ok(await seen(page.locator('#balbox .state-error')), 'la transferencia avisa que el saldo no está disponible');
  ok(await page.locator('#next').isDisabled(), 'y «Continuar» queda deshabilitado hasta que cargue');
  await page.goto(BASE + '/sitios/banco/#/movimientos');
  ok(await seen(page.locator('#mv .state-error')), 'con core-bancario caído no se ven los movimientos');
  await restore('banco');

  await fault('banco', 'motor-antifraude');
  await transfer();
  ok(await seen(page.locator('.err-box').filter({ hasText: 'No pudimos realizar tu transferencia' })), 'con motor-antifraude caído la transferencia se rechaza y el saldo se ve');
  await restore('banco');
  await page.close();

  console.log('Clínica');
  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/');
  const book = async () => {
    await page.click('#b-go');
    await page.locator('.slot button').first().click();
  };
  await book();
  await page.fill('input[name=rut]', '11.111.111-1'); await page.fill('input[name=pass]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Hora confirmada' })), 'reserva de hora confirmada con comprobante (TC-06)');
  await page.reload();
  await fault('clinica', 'ficha-clinica-db');
  await book();
  await page.fill('input[name=rut]', '11.111.111-1'); await page.fill('input[name=pass]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('.err-box').filter({ hasText: 'No pudimos confirmar tu hora' })), 'con la ficha clínica caída la reserva falla (TC-07/08)');
  await restore('clinica');
  await page.close();

  console.log('Pantallas adicionales');
  page = await newPage();
  // Con el catálogo lento, la ficha muestra el esqueleto mientras responde, nunca un error.
  await faultKind('mercado', 'api-catalogo', 'degraded', { latency_ms: 1500 });
  await page.goto(BASE + '/sitios/mercado/#/producto/p3');
  ok(await seen(page.locator('#pdp .skeleton'), 1000) && (await page.locator('#pdp .state-error').count()) === 0, 'mientras el catálogo responde, la ficha muestra el esqueleto y no un error');
  ok(await seen(page.locator('.pdp h1').filter({ hasText: 'Manzanas' })), 'la ficha de producto se abre por su dirección');
  await restore('mercado');
  await page.locator('.buy [data-inc="p3"]').click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '1');
  ok((await page.innerText('#cart-count')) === '1', 'se agrega al carro desde la ficha');
  await page.fill('#q', 'palta'); await page.press('#q', 'Enter');
  ok(await seen(page.locator('.row.grid .card').filter({ hasText: 'Palta' })), 'la búsqueda muestra los resultados en grilla y vuelve al inicio');
  await page.close();
  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/#/profesionales');
  await seen(page.locator('.pro')); // la lista llega después de consultar la agenda
  await page.selectOption('#p-spec', 'Pediatría');
  ok(await page.waitForFunction(() => document.querySelectorAll('.pro').length === 2, null, { timeout: 8000 }).then(() => true, () => false), 'el buscador de profesionales filtra por especialidad');
  await page.locator('[data-pro]').first().click();
  ok(await seen(page.locator('.slot button')), 'reservar desde un profesional abre las horas disponibles');
  await page.close();

  console.log('Página de error');
  page = await newPage();
  await fault('mercado', BFF.mercado, 'refused');
  await page.goto(BASE + '/sitios/mercado/');
  ok(await seen(page.locator('.error-card').filter({ hasText: 'Error 502' })), 'con el BFF cerrado el sitio responde 502 con la página de la marca');
  await page.goto(BASE + '/sitios/mercado');
  ok(page.url().endsWith('/sitios/mercado/') && await seen(page.locator('.error-card')), 'sin la barra final redirige a la portada con barra');
  ok(await seen(page.locator('.director-toggle')), 'la página de error conserva el Modo Director');
  await page.click('.director-toggle');
  ok(await seen(page.locator('.director-body [data-all]')), 'y «Restaurar todo» sigue disponible');
  await page.click('.director-body [data-all]');
  ok(await seen(page.locator('.hero'), 15000), 'al restaurar, el sitio vuelve solo en menos de 15 s');
  await page.close();

  for (const [p, svc] of Object.entries(BFF)) {
    if (p === 'mercado') continue;
    page = await newPage();
    await fault(p, svc, '503');
    await page.goto(BASE + `/sitios/${p}/`);
    ok(await seen(page.locator('.error-card').filter({ hasText: 'Error 503' })), `${p}: con el BFF en 503 muestra la página de error`);
    await restore(p);
    await page.click('.error-card #retry');
    ok(await page.waitForFunction(() => !document.querySelector('.error-card'), null, { timeout: 8000 }).then(() => true, () => false), `${p}: «Reintentar ahora» vuelve al sitio`);
    await page.close();
  }

  page = await newPage();
  await faultKind('mercado', BFF.mercado, 'degraded', { latency_ms: 3500 });
  await page.goto(BASE + '/sitios/mercado/');
  ok(await seen(page.locator('.error-card').filter({ hasText: 'Error 504' })), 'con el BFF lento la página de error trae el 504');
  await restore('mercado');
  await page.close();

  console.log('Servicios caídos en los sitios');
  // Mercado: sin auth se entra como invitado; un salto agotado se ve como 504.
  page = await newPage();
  await page.goto(BASE + '/sitios/mercado/');
  await seen(page.locator('[data-inc="p1"]'));
  await fault('mercado', 'auth-clientes', '503');
  await page.click('#btn-login');
  await page.fill('.modal-back input[name=u]', 'cliente@demo.cl');
  await page.fill('.modal-back input[name=p]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('.modal-back .err-box').filter({ hasText: 'No pudimos iniciar tu sesión' })), 'con auth-clientes caído falla el ingreso');
  await page.locator('.modal-back').click({ position: { x: 5, y: 5 } });
  await page.locator('[data-inc="p1"]').first().click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '1');
  await page.click('#btn-cart');
  await page.click('#pay');
  ok(await seen(page.locator('.ok-box').filter({ hasText: 'Pago aprobado' })), 'y el pago como invitado se aprueba (D-15)');
  await restore('mercado');
  await page.close();

  page = await newPage();
  await page.goto(BASE + '/sitios/mercado/');
  await seen(page.locator('[data-inc="p1"]'));
  await page.locator('[data-inc="p1"]').first().click();
  await page.waitForFunction(() => document.querySelector('#cart-count').textContent === '1');
  await faultKind('mercado', 'servicio-inventario', 'degraded', { latency_ms: 3500 });
  await page.click('#btn-cart');
  await page.click('#pay');
  ok(await seen(page.locator('.err-box').filter({ hasText: 'Error 504' })), 'con la reserva en 3.500 ms el pago muestra el 504');
  ok((await page.locator('.err-box').innerText()).includes('servicio-inventario'), 'y nombra al servicio lento, no al primer salto');
  await restore('mercado');
  await page.close();

  page = await newPage();
  await faultKind('mercado', 'api-catalogo', 'errors', { error_rate: 1 });
  await page.goto(BASE + '/sitios/mercado/');
  const err500 = page.locator('#shelves .state-error').filter({ hasText: 'Error 500' });
  ok(await seen(err500) && (await err500.innerText()).includes('api-catalogo'), 'con api-catalogo fallando el 100 % los estantes muestran el 500 y el servicio');
  await restore('mercado');
  await page.close();

  // Clínica: ingreso, búsqueda, profesionales y exámenes.
  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/');
  await fault('clinica', 'auth-pacientes', '503');
  await page.click('#btn-mi');
  await page.fill('.modal-back input[name=rut]', '11.111.111-1');
  await page.fill('.modal-back input[name=pass]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('.modal-back .err-box').filter({ hasText: 'No pudimos iniciar tu sesión' })), 'con auth-pacientes caído falla el ingreso a Mi Clínica');
  await restore('clinica');
  await page.reload();
  await page.close();

  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/');
  await fault('clinica', 'agenda-medica', '503');
  await page.click('#b-go');
  ok(await seen(page.locator('.modal-back .err-box').filter({ hasText: 'No pudimos buscar horas disponibles' })), 'con agenda-medica caída falla «Buscar horas»');
  await page.locator('.modal-back').click({ position: { x: 5, y: 5 } });
  await page.goto(BASE + '/sitios/clinica/#/profesionales');
  ok(await seen(page.locator('#p-list .state-error').filter({ hasText: 'No pudimos cargar la disponibilidad de profesionales' })), 'y el buscador de profesionales muestra el error');
  await restore('clinica');
  await page.close();

  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/');
  await fault('clinica', 'api-laboratorio', '503');
  await page.click('#btn-mi');
  await page.fill('.modal-back input[name=rut]', '11.111.111-1');
  await page.fill('.modal-back input[name=pass]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('#ex-list .state-error').filter({ hasText: 'No pudimos cargar tus exámenes' })), 'con api-laboratorio caído «Mis exámenes» muestra el error');
  await restore('clinica');
  await page.close();

  page = await newPage();
  await page.goto(BASE + '/sitios/clinica/');
  await fault('clinica', 'ficha-clinica-db', '503');
  await page.click('#b-go');
  ok(await seen(page.locator('.slot button')), 'con la ficha caída «Buscar horas» sigue funcionando');
  await page.locator('.modal-back').click({ position: { x: 5, y: 5 } });
  await page.click('#btn-mi');
  await page.fill('.modal-back input[name=rut]', '11.111.111-1');
  await page.fill('.modal-back input[name=pass]', 'demo1234');
  await page.click('.modal-back form button');
  ok(await seen(page.locator('#ex-list .state-error')), 'y los exámenes no cargan');
  await restore('clinica');
  await page.close();

  console.log('Modo Director');
  page = await newPage();
  await page.goto(BASE + '/sitios/mercado/');
  await page.click('.director-toggle');
  await page.locator('.director-body tr', { hasText: 'pasarela-pagos' }).locator('button', { hasText: 'Cerrar puerto' }).click();
  await page.waitForTimeout(600);
  const f = await ctl('GET', '/faults');
  ok(f.some((x) => x.service === 'pasarela-pagos' && x.params.mode === 'refused'), 'el botón «Cerrar puerto» aplica la falla en el servicio elegido');
  await page.click('.director-body [data-all]');
  await page.waitForTimeout(600);
  ok((await ctl('GET', '/faults')).length === 0, '«Restaurar todo» deja la plataforma sana (TC-14)');
  await page.close();

  ok(errors.length === 0, 'sin errores de JavaScript en ninguna página' + (errors.length ? ': ' + errors.join(' | ') : ''));
  await browser.close();
  console.log(failed ? `\n${failed} de ${total} verificaciones fallaron` : `\ntodo en orden: ${total} verificaciones`);
  process.exit(failed ? 1 : 0);
})();
