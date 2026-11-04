import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const script = fs.readFileSync(new URL('../cmd/vault/pki-agility/performance.js', import.meta.url), 'utf8');
const pureFunctions = script.slice(0, script.indexOf('async function reportFramedNavigation'));
const context = {};
vm.runInNewContext(`${pureFunctions}\nthis.sample = navigationSample; this.median = median; this.roundModes = roundModes;`, context);

assert.equal(context.median([7, 1, 4]), 4);
assert.equal(context.median([7, 1, 4, 2]), 3);
assert.equal(context.median([]), null);
const positions = Object.fromEntries(['traditional', 'hybrid', 'pqc', 'pure'].map(name => [name, [0, 0, 0, 0]]));
for (let round = 0; round < 8; round++) {
  const names = Array.from(context.roundModes(round), mode => mode.name);
  assert.equal(new Set(names).size, 4);
  names.forEach((name, position) => positions[name][position]++);
}
for (const counts of Object.values(positions)) assert.deepEqual(counts, [2, 2, 2, 2]);

const fresh = context.sample({connectStart: 1, secureConnectionStart: 2,
  connectEnd: 5, requestStart: 6, responseStart: 10});
assert.equal(fresh.connection, 4);
assert.equal(fresh.tls, 3);
assert.equal(fresh.ttfb, 4);

for (const entry of [
  null,
  {connectStart: 0, secureConnectionStart: 0, connectEnd: 0, requestStart: 1, responseStart: 2},
  {connectStart: 1, secureConnectionStart: 0, connectEnd: 1, requestStart: 2, responseStart: 3},
  {connectStart: 1, secureConnectionStart: 2, connectEnd: 5, requestStart: 6, responseStart: 6},
]) {
  const sample = context.sample(entry);
  assert.equal(sample.connection, null);
  assert.equal(sample.tls, null);
  assert.equal(sample.ttfb, null);
}
console.log('browser navigation timing classification passed');
