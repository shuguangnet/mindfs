import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

const main = read("../src/main.tsx");
const e2ee = read("../src/services/e2ee.ts");
const fileTree = read("../src/components/FileTree.tsx");
const authService = read("../src/services/auth.ts");
const authLogin = read("../src/components/AuthLogin.tsx");
const settingsDialog = read("../src/components/AuthSettingsDialog.tsx");
const zh = read("../src/i18n/locales/zh-CN.ts");
const en = read("../src/i18n/locales/en-US.ts");

// The browser entry must gate the app behind the login screen before App mounts.
assert.match(main, /import \{ AuthLogin \}/, "main.tsx should import the auth login screen");
assert.match(main, /fetchAuthStatus\(\)/, "main.tsx should query the auth status");
assert.match(main, /<AuthLogin onSuccess=/, "main.tsx should render AuthLogin when locked");
assert.match(main, /subscribeUnauthenticated\(/, "main.tsx should react to global 401s");

// Global 401 handling: a rejected request must notify the app to return to login.
assert.match(e2ee, /notifyUnauthenticated/, "e2ee.ts should surface unauthenticated responses");
assert.match(
  e2ee,
  /error[\s\S]{0,40}=== "unauthenticated"/,
  "only unauthenticated errors should trigger the login redirect",
);

// The account menu lives in the top bar and offers auth settings + logout.
assert.match(fileTree, /<AuthMenuButton \/>/, "FileTree top bar should render AuthMenuButton");
assert.match(authService, /AUTH_UNAUTHENTICATED_EVENT/, "auth service should define the event name");
assert.match(authLogin, /loginAuth\(/, "AuthLogin should call the login API");
assert.match(settingsDialog, /saveAuthSettings\(/, "AuthSettingsDialog should save via the API");

// i18n: every auth key must exist in both locales.
const keys = (source) => {
  const matches = source.matchAll(/"(auth\.[a-zA-Z0-9.]+)":/g);
  return new Set([...matches].map((match) => match[1]));
};
const zhKeys = keys(zh);
const enKeys = keys(en);
assert.ok(zhKeys.size >= 20, `expected a full auth key set, got ${zhKeys.size}`);
for (const key of zhKeys) {
  assert.ok(enKeys.has(key), `en-US is missing translation key ${key}`);
}
for (const key of enKeys) {
  assert.ok(zhKeys.has(key), `zh-CN is missing translation key ${key}`);
}

console.log("auth-gate.test.mjs passed");
