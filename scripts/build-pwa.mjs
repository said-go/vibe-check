import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, unlinkSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const dist = resolve(root, 'dist');
const check = process.argv.includes('--check');
const hash = bytes => createHash('sha256').update(bytes).digest('hex').slice(0, 16);
// Git may use CRLF on Windows and LF in CI. Hash identical logical text identically.
const readCanonical = path => path.endsWith('.png') ? readFileSync(path) : Buffer.from(readFileSync(path, 'utf8').replace(/\r\n/g, '\n'));
const sources = ['styles.css', 'history.js', 'share-card.js', 'app.js'];
const generated = new Map();
let html = readCanonical(resolve(dist, 'index.html')).toString();
for (const name of sources) {
  const bytes = readCanonical(resolve(dist, name));
  const [base, ext] = name.split('.');
  const asset = `assets/${base}.${hash(bytes)}.${ext}`;
  generated.set(asset, bytes);
  const pattern = new RegExp(`(?:assets/)?${base}(?:\\.[a-f0-9]{16})?\\.${ext}`, 'g');
  html = html.replace(pattern, asset);
}
generated.set('index.html', Buffer.from(html));
const shell = ['/index.html', ...[...generated.keys()].filter(x => x !== 'index.html').map(x => `/${x}`), '/manifest.webmanifest', '/offline.html', '/icons/icon-192.png', '/icons/icon-512.png', '/icons/icon-maskable-512.png'];
const release = hash(Buffer.concat(shell.map(path => generated.get(path.slice(1)) || readCanonical(resolve(dist, path.slice(1))))));
const sw = readCanonical(resolve(dist, 'sw.js')).toString()
  .replace(/const CACHE_NAME = .*?;/, `const CACHE_NAME = 'vibe-check-${release}';`)
  .replace(/const APP_SHELL = .*?;/, `const APP_SHELL = ${JSON.stringify(shell)};`);
generated.set('sw.js', Buffer.from(sw));
if (!check) mkdirSync(resolve(dist, 'assets'), { recursive: true });
let stale = false;
for (const [path, data] of generated) {
  const target = resolve(dist, path);
  if (!existsSync(target) || !readCanonical(target).equals(data)) {
    if (check) { console.error(`PWA build is stale: ${path}`); stale = true; }
    else writeFileSync(target, data);
  }
}
if (!check) for (const name of readdirSync(resolve(dist, 'assets'))) {
  if (/^[a-z-]+\.[a-f0-9]{16}\.(js|css)$/.test(name) && !generated.has(`assets/${name}`)) unlinkSync(resolve(dist, 'assets', name));
}
if (stale) process.exitCode = 1;
else console.log(`PWA release ${release} ${check ? 'verified' : 'built'}`);
