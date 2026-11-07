// Load test for the performance targets of design 04 §5.4 (ADR 0038). Run it against a
// site seeded by internal/tools/benchseed:
//
//   k6 run -e BASE=http://127.0.0.1:17546 bench/k6/game.js
//
// Each scenario runs at a fixed rate, so the thresholds measure latency under a steady
// load rather than the most a single machine can push. DURATION (default 60s) and
// RATE (a multiplier, default 1) tune it. The thresholds are the targets: k6 exits
// non-zero when one is missed.

import http from "k6/http";
import exec from "k6/execution";
import { check } from "k6";

const BASE = (__ENV.BASE || "http://127.0.0.1:17546").replace(/\/+$/, "");
const DURATION = __ENV.DURATION || "60s";
const RATE = Number(__ENV.RATE || 1);
const PLAYERS = Number(__ENV.PLAYERS || 100000); // benchseed -players
const FILES = Number(__ENV.FILES || 64); // benchseed -files
const PASSWORD = "bench-pass-1";

const SIZES = [16, 24, 32, 48, 64, 96, 100, 128, 256, 512];
const RENDERS = FILES * SIZES.length * 2; // file × size × 2D/3D, each rendered once

function steady(rate, start, exec_) {
  return {
    executor: "constant-arrival-rate",
    rate: Math.max(1, Math.round(rate * RATE)),
    timeUnit: "1s",
    duration: DURATION,
    preAllocatedVUs: 50,
    maxVUs: 400,
    startTime: start,
    exec: exec_,
  };
}

// The avatar cache is warmed by avatar_miss (every render once) before avatar_hit.
const missTime = "40s";

export const options = {
  discardResponseBodies: true,
  scenarios: {
    avatar_miss: { executor: "shared-iterations", vus: 8, iterations: RENDERS, maxDuration: missTime, exec: "avatarMiss" },
    csl: steady(500, missTime, "csl"),
    usm_legacy: steady(100, missTime, "usmLegacy"),
    texture_file: steady(200, missTime, "textureFile"),
    avatar_hit: steady(200, missTime, "avatarHit"),
    has_joined: steady(50, missTime, "joinAndCheck"),
    skinlib: steady(50, missTime, "skinlib"),
  },
  thresholds: {
    "http_req_duration{scenario:csl}": ["p(99)<5"],
    "http_req_duration{scenario:usm_legacy}": ["p(99)<5"],
    "http_req_duration{scenario:texture_file}": ["p(99)<5"],
    "http_req_duration{scenario:avatar_hit}": ["p(99)<10"],
    "http_req_duration{scenario:avatar_miss}": ["p(99)<50"],
    "http_req_duration{scenario:has_joined,name:hasJoined}": ["p(99)<20"],
    "http_req_duration{scenario:skinlib}": ["p(99)<50"],
    checks: ["rate>0.999"],
  },
  summaryTrendStats: ["avg", "med", "p(90)", "p(99)", "max"],
};

const rnd = (n) => Math.floor(Math.random() * n);
const name = (i) => "P" + String(i).padStart(6, "0");
// Player i wears skin file i % FILES: players below FILES cover every file.
const ok = (r, status) => check(r, { [`status ${status}`]: (x) => x.status === status });

export function setup() {
  // Hashes of the files players wear, for the file scenario.
  const hashes = [];
  for (let i = 0; i < FILES; i++) {
    const r = http.get(`${BASE}/csl/${name(i)}.json`, { responseType: "text" });
    const skins = JSON.parse(r.body).skins;
    hashes.push(skins.default || skins.slim);
  }
  // A launcher session for player P000000 (account 1).
  const r = http.post(
    `${BASE}/api/yggdrasil/authserver/authenticate`,
    JSON.stringify({ username: name(0), password: PASSWORD, clientToken: "k6" }),
    { headers: { "Content-Type": "application/json" }, responseType: "text" },
  );
  if (r.status !== 200) throw new Error(`authenticate: ${r.status} ${r.body}`);
  const auth = JSON.parse(r.body);
  return { hashes, token: auth.accessToken, uuid: auth.selectedProfile.id, player: auth.selectedProfile.name };
}

export function csl() {
  ok(http.get(`${BASE}/${name(rnd(PLAYERS))}.json`), 200);
}

export function usmLegacy() {
  const n = name(rnd(PLAYERS));
  ok(http.get(Math.random() < 0.5 ? `${BASE}/usm/${n}.json` : `${BASE}/skin/${n}.png`), 200);
}

export function textureFile(data) {
  ok(http.get(`${BASE}/textures/${data.hashes[rnd(data.hashes.length)]}`), 200);
}

function renderURL(i) {
  const file = i % FILES;
  const size = SIZES[Math.floor(i / FILES) % SIZES.length];
  const threeD = Math.floor(i / (FILES * SIZES.length)) % 2 === 1;
  return `${BASE}/avatar/player/${name(file)}?size=${size}${threeD ? "&3d" : ""}`;
}

export function avatarMiss() {
  ok(http.get(renderURL(exec.scenario.iterationInTest), { headers: { Accept: "image/webp" } }), 200);
}

export function avatarHit() {
  ok(http.get(renderURL(rnd(RENDERS)), { headers: { Accept: "image/webp" } }), 200);
}

export function joinAndCheck(data) {
  const serverId = String(rnd(1e9)) + "-" + exec.vu.idInTest + "-" + exec.scenario.iterationInTest;
  const j = http.post(
    `${BASE}/api/yggdrasil/sessionserver/session/minecraft/join`,
    JSON.stringify({ accessToken: data.token, selectedProfile: data.uuid, serverId }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "join" } },
  );
  ok(j, 204);
  const h = http.get(
    `${BASE}/api/yggdrasil/sessionserver/session/minecraft/hasJoined?username=${data.player}&serverId=${serverId}`,
    { tags: { name: "hasJoined" } },
  );
  ok(h, 200);
}

export function skinlib() {
  const sort = Math.random() < 0.5 ? "time" : "likes";
  const category = ["", "skin", "steve", "alex", "cape"][rnd(5)];
  const r = http.get(`${BASE}/api/v1/textures?sort=${sort}&category=${category}&limit=20`, { responseType: "text", tags: { name: "list" } });
  ok(r, 200);
  // Half the visitors open a later page.
  if (r.status === 200 && Math.random() < 0.5) {
    const next = JSON.parse(r.body).next;
    if (next) {
      ok(http.get(`${BASE}/api/v1/textures?sort=${sort}&category=${category}&limit=20&cursor=${encodeURIComponent(next)}`, { tags: { name: "list" } }), 200);
    }
  }
}
