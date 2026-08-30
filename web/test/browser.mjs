import assert from "node:assert/strict";
import { createReadStream, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";
import { argv } from "node:process";
import AxeBuilder from "@axe-core/playwright";
import { chromium } from "playwright";

const [siteDir, corpusDir] = argv.slice(2);
if (!siteDir || !corpusDir) {
  console.error("usage: node web/test/browser.mjs <site-dir> <corpus-dir>");
  process.exit(2);
}

const types = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".wasm": "application/wasm", ".jhtc": "application/octet-stream" };
const server = createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  const inCorpus = url.pathname.startsWith("/corpus/");
  const root = inCorpus ? corpusDir : siteDir;
  const relative = inCorpus ? url.pathname.slice(8) : (url.pathname === "/" ? "index.html" : url.pathname.slice(1));
  const path = normalize(join(root, relative));
  if (!path.startsWith(normalize(root)) || !existsSync(path)) {
    response.writeHead(404).end("not found");
    return;
  }

  const size = statSync(path).size;
  const range = request.headers.range?.match(/^bytes=(\d+)-(\d+)$/);
  const headers = { "Content-Type": types[extname(path)] || "application/octet-stream", "Accept-Ranges": "bytes", "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" };
  if (range) {
    const start = Number(range[1]);
    const end = Math.min(Number(range[2]), size - 1);
    response.writeHead(206, { ...headers, "Content-Length": end - start + 1, "Content-Range": `bytes ${start}-${end}/${size}` });
    createReadStream(path, { start, end }).pipe(response);
  } else {
    response.writeHead(200, { ...headers, "Content-Length": size });
    if (request.method === "HEAD") response.end();
    else createReadStream(path).pipe(response);
  }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const corpus = `${origin}/corpus/`;
const browser = await chromium.launch({ headless: true });

async function ready(page, query = "") {
  await page.goto(`${origin}/?corpus=${encodeURIComponent(corpus)}${query ? `&${query}` : ""}`);
  await page.locator("#go").waitFor({ state: "visible", timeout: 30_000 });
  await page.waitForTimeout(500);
  await assertNoOverflow(page);
}

async function assertNoOverflow(page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  assert.ok(overflow <= 1, `horizontal overflow: ${overflow}px`);
}

async function assertAxe(page, label) {
  const results = await new AxeBuilder({ page }).analyze();
  const violations = results.violations.map((violation) => `${violation.id}: ${violation.nodes.length}`);
  assert.deepEqual(violations, [], `${label} axe violations: ${results.violations[0]?.nodes[0]?.failureSummary || ""}`);
}

try {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: "dark", reducedMotion: "reduce" });
  const page = await context.newPage();
  page.on("pageerror", (error) => console.error(`browser page error: ${error.message}`));

  // Loading/readiness remains understandable and does not announce the ticker.
  const loading = page.goto(`${origin}/?corpus=${encodeURIComponent(corpus)}&qv=1&state=open&state=stale&state=closed&state=lapsed&page=2`);
  await page.locator("#filters").waitFor();
  assert.equal(await page.locator("#list").getAttribute("aria-busy"), "true");
  assert.equal(await page.locator("#stage").getAttribute("aria-hidden"), "true");
  await loading;
  await page.locator("#go").waitFor({ state: "visible", timeout: 30_000 });

  // At most one bounded page exists in the DOM. Position survives reload and
  // participates in Back without pretending to be a durable cross-generation cursor.
  assert.equal(await page.locator("#page-number").inputValue(), "2");
  assert.ok(await page.locator(".card:not(.skeleton)").count() <= 100);
  await page.reload();
  await page.locator("#go").waitFor({ state: "visible", timeout: 30_000 });
  assert.equal(await page.locator("#page-number").inputValue(), "2");
  await page.locator("#previous").click();
  await page.waitForFunction(() => !new URLSearchParams(location.search).has("page"));
  assert.equal(await page.locator("#count").evaluate((node) => node === document.activeElement), true);
  await page.locator("#next").click();
  await page.waitForFunction(() => new URLSearchParams(location.search).get("page") === "2");
  await page.goBack();
  await page.waitForFunction(() => !new URLSearchParams(location.search).has("page"));

  // Changing a filter mid-page resets to page one; rapid navigation never
  // leaves more than one page or duplicate nodes behind.
  await page.locator("#next").click();
  await page.locator("#f-title").fill("engineer");
  await page.waitForTimeout(250);
  assert.equal(new URL(page.url()).searchParams.has("page"), false);
  assert.ok(await page.locator(".card:not(.skeleton)").count() <= 100);

  await page.keyboard.press("Escape");
  await page.locator("body").click({ position: { x: 2, y: 2 } });
  await page.keyboard.press("/");
  assert.equal(await page.locator("#f-title").evaluate((node) => node === document.activeElement), true);
  assert.match(await page.locator("#shortcut-help").textContent(), /slash.*Escape/i);

  assert.equal(await page.locator("time[aria-label]").count(), 0);
  const reduced = await page.locator(".card").first().evaluate((node) => getComputedStyle(node).animationDuration);
  assert.ok(["0.00001s", "1e-05s", "0s"].includes(reduced), `reduced motion animation: ${reduced}`);
  await assertAxe(page, "dark mobile results");
  await assertNoOverflow(page);

  // Empty results keep one useful recovery action and a concise live message.
  await page.locator("#f-title").fill("definitely-no-such-fixture-role-zzzz");
  await page.waitForFunction(() => document.querySelector("#count")?.textContent === "No postings match.");
  assert.equal(await page.locator(".empty button").textContent(), "Clear filters");

  // Warm service-worker navigation reaches readiness without duplicate engine payloads.
  await page.reload();
  await page.locator("#go").waitFor({ state: "visible", timeout: 30_000 });
  const wasmLoads = await page.evaluate(() => performance.getEntriesByName(new URL("engine.wasm", location.href).href).length);
  assert.ok(wasmLoads <= 1, `engine.wasm loaded ${wasmLoads} times`);
  await context.close();

  // Native GET is honest: named controls serialize query intent even though
  // noscript explains that local Wasm is required to execute it.
  const nativeContext = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 360, height: 800 } });
  const native = await nativeContext.newPage();
  await native.goto(origin);
  await native.locator("#f-title").fill("security engineer");
  await Promise.all([native.waitForNavigation(), native.locator("#filters").evaluate((form) => form.requestSubmit())]);
  const nativeURL = new URL(native.url());
  assert.equal(nativeURL.searchParams.get("qv"), "1");
  assert.equal(nativeURL.searchParams.get("title"), "security engineer");
  assert.match(await native.locator("noscript").textContent(), /JavaScript and WebAssembly/);
  await assertNoOverflow(native);
  await nativeContext.close();

  // Representative tablet and desktop layouts remain bounded and accessible.
  for (const width of [768, 1280]) {
    const layout = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: "light" });
    const p = await layout.newPage();
    await ready(p, "qv=1&state=open&state=stale&state=closed&state=lapsed");
    await assertAxe(p, `${width}px results`);
    await layout.close();
  }

  // Failure state is focused, announced and still free of serious violations.
  const failureContext = await browser.newContext({ viewport: { width: 360, height: 800 } });
  const failure = await failureContext.newPage();
  await failure.goto(`${origin}/?corpus=${encodeURIComponent(`${origin}/missing/`)}`);
  await failure.locator("#error").waitFor({ state: "visible", timeout: 10_000 });
  assert.equal(await failure.locator("#error").evaluate((node) => node === document.activeElement), true);
  await assertAxe(failure, "error state");
  await assertNoOverflow(failure);
  await failureContext.close();

  console.log("Browser URL, history, keyboard, focus, axe, layout, readiness and pagination tests passed");
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}
