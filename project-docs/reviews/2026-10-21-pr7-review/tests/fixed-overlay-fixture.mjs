import http from 'node:http';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve, join } from 'node:path';

if (!process.argv[2]) throw new Error('Usage: node fixed-overlay-fixture.mjs <repo-root>');
const root = resolve(process.argv[2]);
const assets = join(root, 'backend/internal/web/dist/assets');
const styles = readdirSync(assets).filter(name => name.startsWith('index-') && name.endsWith('.css'));
if (!styles.length) throw new Error('Build the target frontend first; production CSS is missing.');
const template = readFileSync(join(root, 'frontend/src/components/user/profile/TotpSetupModal.vue'), 'utf8');
const layout = readFileSync(join(root, 'frontend/src/components/layout/AppLayout.vue'), 'utf8');
const overlay = template.match(/<template>\s*(<div\b[^>]*>)/)[1].replace(/\s+@click\.self="[^"]*"/, '');
const main = layout.match(/<main\b[^>]*>/)[0].replace(/>$/, ' style="height:1600px">');
const body = `<!doctype html><html><head>${styles.map(name => `<link rel="stylesheet" href="/assets/${name}">`).join('')}</head><body><aside class="sidebar w-64">Sidebar</aside><div style="margin-left:15rem"><header style="height:56px">Header</header>${main}${overlay}<div class="flex min-h-full items-center justify-center p-4"><section id="dialog-panel" style="width:300px;height:120px;background:white">TotpSetupModal fixed overlay fixture</section></div></div></main></div></body></html>`;
http.createServer((req, res) => {
  if (req.url === '/without-animation.html') {
    res.setHeader('Content-Type', 'text/html'); res.end(body.replace(/\bmotion-page\b/g, '')); return;
  }
  if (req.url === '/' || req.url === '/review-check.html') {
    res.setHeader('Content-Type', 'text/html'); res.end(body); return;
  }
  const name = decodeURIComponent(req.url.slice('/assets/'.length));
  if (req.url.startsWith('/assets/') && styles.includes(name)) {
    res.setHeader('Content-Type', 'text/css'); res.end(readFileSync(join(assets, name))); return;
  }
  res.statusCode = 404; res.end();
}).listen(18747, '127.0.0.1', () => {
  console.log('Open http://127.0.0.1:18747/review-check.html and /without-animation.html');
});
