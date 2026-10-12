// Golden data for the Go analytics port: runs the demo's own trips.ts / sites.ts on
// deterministic synthetic tracks and stores input + expected output.
//
//   node internal/analytics/testdata/gen_golden.ts   (needs ../fmctest next to fms-backend)
import fs from 'node:fs';
import path from 'node:path';
import { summarizeDay } from '../../../../fmctest/server/trips.ts';
import { buildReport, routeHistory } from '../../../../fmctest/server/sites.ts';

// Deterministic PRNG (mulberry32).
function rng(seed: number) {
  return () => {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const L = { lat: -6.2, lng: 106.8 };
const U = { lat: -6.23, lng: 106.84 };
const P = { lat: -6.215, lng: 106.86 }; // a parking spot

function track(seed: number, start: number) {
  const r = rng(seed);
  const recs: any[] = [];
  let t = start;
  let fuel = 4000;
  const push = (lat: number, lng: number, speed: number, ign: number, extra: Record<string, number> = {}) => {
    const io: Record<string, number> = { 239: ign, 66: 13800, 84: Math.round(fuel), ...extra };
    recs.push({ time: t, priority: 0, lat, lng, alt: 10, angle: Math.floor(r() * 360), sats: 10, speed, eventIo: 0, valid: !(r() < 0.02), io });
  };
  const stay = (p: { lat: number; lng: number }, mins: number, ign: number) => {
    for (let i = 0; i < mins * 2; i++) {
      push(p.lat + (r() - 0.5) * 0.00004, p.lng + (r() - 0.5) * 0.00004, r() < 0.1 ? 3 : 0, ign);
      t += 30000;
    }
  };
  const drive = (a: { lat: number; lng: number }, b: { lat: number; lng: number }, steps: number) => {
    for (let i = 1; i <= steps; i++) {
      const k = i / steps;
      const jam = r() < 0.05;
      push(a.lat + (b.lat - a.lat) * k + (r() - 0.5) * 0.0002, a.lng + (b.lng - a.lng) * k + (r() - 0.5) * 0.0002, jam ? 0 : 20 + Math.floor(r() * 50), 1);
      fuel -= 2 + r() * 3;
      t += jam ? 90000 : 15000;
    }
  };
  stay(P, 20, 0);
  for (let lap = 0; lap < 3; lap++) {
    drive(lap === 0 ? P : U, L, 40);
    stay(L, 4 + Math.floor(r() * 4), 1);
    drive(L, U, 60);
    stay(U, 3, 1);
    if (lap === 1) { fuel += 600; } // refuel
  }
  stay(U, 30, 0);
  fuel -= 120; // drop while parked
  stay(U, 10, 0);
  drive(U, L, 40); // tail: still on the way back
  return recs;
}

const now = 1767225600000 + 12 * 3600e3;
const sites = [
  { id: 'site-l', name: 'Loading A', lat: L.lat, lng: L.lng, radius: 60, note: null, imeis: [], createdAt: 0, updatedAt: 0 },
  { id: 'site-u', name: 'Unloading B', lat: U.lat, lng: U.lng, radius: 40, note: null, imeis: [], createdAt: 0, updatedAt: 0 },
];
const routes = [{ id: 'route-1', name: 'A → B', loadingId: 'site-l', unloadingId: 'site-u', note: null, onDuty: [], imeis: [], createdAt: 0, updatedAt: 0 }];

const cases = [1, 2, 3].map((seed) => {
  const start = 1767225600000 + seed * 600000;
  const records = track(seed, start);
  const from = start;
  const to = records[records.length - 1].time + 60000;
  return {
    seed, from, to, now, records,
    day: summarizeDay(records, from, to),
    report: buildReport(records, routes, sites, from, to, now),
  };
});
const perTruck = cases.map((c) => ({ imei: `35000000000000${c.seed}`, records: c.records }));
const history = routeHistory(routes[0], sites, perTruck, cases[0].from, cases[2].to, 1);

fs.writeFileSync(path.join(import.meta.dirname, 'golden.json'), JSON.stringify({ sites, routes, cases, history }));
console.log('golden.json:', cases.map((c) => `${c.records.length} records, ${c.report.trips.length} trips`).join(' | '));
