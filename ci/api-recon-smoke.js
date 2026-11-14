// Isolated smoke test for the api-recon Puppeteer runtime, run inside the built
// image in CI (never on a trusted workstation: it launches Chromium as root with
// --no-sandbox). It exercises the exact puppeteer-core calls runtime_harvest.js
// makes, so a puppeteer-core/Chromium version skew surfaces here rather than at
// scan time.
//
//   docker run --rm --shm-size=1g -v "$PWD/ci:/ci:ro" \
//     --entrypoint node <image> /ci/api-recon-smoke.js
const modulePath = process.env.ARTEX_PUPPETEER_PATH || '/opt/api-recon-runtime/node_modules/puppeteer-core';
const chromium = process.env.CHROMIUM || '/usr/bin/chromium';
const puppeteer = require(modulePath);

(async () => {
  const version = require(modulePath + '/package.json').version;
  const browser = await puppeteer.launch({
    executablePath: chromium,
    headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--ignore-certificate-errors'],
  });
  try {
    const page = await browser.newPage();
    const cdp = await page.target().createCDPSession();
    await cdp.send('Network.enable');
    await page.setRequestInterception(true);
    let seen = 0;
    page.on('request', (r) => { seen++; r.continue(); });
    await page.evaluateOnNewDocument(() => { window.__ARTEX_SMOKE__ = 1; });
    await page.goto('data:text/html,<title>ok</title>', { waitUntil: 'domcontentloaded' });
    const title = await page.title();
    const flag = await page.evaluate(() => window.__ARTEX_SMOKE__);
    if (title !== 'ok' || flag !== 1 || seen < 1) {
      throw new Error(`smoke assertions failed: title=${title} flag=${flag} requests=${seen}`);
    }
    console.log(`api-recon puppeteer smoke: ok (puppeteer-core ${version}, chromium ${chromium}, requests ${seen})`);
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e && e.stack ? e.stack : String(e)); process.exit(1); });
