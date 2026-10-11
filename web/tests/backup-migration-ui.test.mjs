import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const dialog = readFileSync(
  new URL("../src/components/BackupDialog.tsx", import.meta.url),
  "utf8",
);
const service = readFileSync(
  new URL("../src/services/backup.ts", import.meta.url),
  "utf8",
);
const fileTree = readFileSync(
  new URL("../src/components/FileTree.tsx", import.meta.url),
  "utf8",
);
const sessionList = readFileSync(
  new URL("../src/components/SessionList.tsx", import.meta.url),
  "utf8",
);
const app = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
const zhCN = readFileSync(
  new URL("../src/i18n/locales/zh-CN.ts", import.meta.url),
  "utf8",
);
const enUS = readFileSync(
  new URL("../src/i18n/locales/en-US.ts", import.meta.url),
  "utf8",
);

// Service covers all endpoints used by the dialog.
for (const endpoint of [
  "/api/backup/export",
  "/api/backup/import",
  "/api/backup/apply",
  "export-native",
]) {
  assert.ok(service.includes(endpoint), `service must call ${endpoint}`);
}

// Dialog wires the three-phase flow with per-root actions.
for (const needle of [
  "exportBackupBundle",
  "importBackupBundle",
  "applyBackupImport",
  "path_override",
  "include_device_identity",
  "downloadBlob",
]) {
  assert.ok(dialog.includes(needle), `dialog must reference ${needle}`);
}

// Sidebar entry registered next to the SSH servers entry.
assert.ok(fileTree.includes("BackupDialog"), "FileTree must mount BackupDialog");
assert.ok(fileTree.includes('"backup.title"'), "FileTree must label the entry with backup.title");

// Session menu offers native pi export for pi sessions, wired in App.
assert.ok(sessionList.includes("sessionList.exportNativePi"), "session row menu must include native export");
assert.ok(sessionList.includes('"pi"'), "native export gated on pi agent");
assert.ok(app.includes("handleExportNativeSession"), "App must provide the handler");
assert.ok(app.includes("exportSessionNative"), "App must import the service");

// i18n parity for the new keys.
for (const key of [
  "backup.title",
  "backup.exportButton",
  "backup.decryptPreview",
  "backup.applyButton",
  "backup.actionMerge",
  "backup.actionReplace",
  "backup.actionSkip",
  "backup.statusNew",
  "backup.statusExists",
  "backup.statusMissing",
  "backup.restartHint",
  "session.exportNativeDone",
  "session.exportNativeExisted",
  "session.exportNativeFailed",
  "sessionList.exportNativePi",
]) {
  assert.ok(zhCN.includes(`"${key}"`), `zh-CN missing ${key}`);
  assert.ok(enUS.includes(`"${key}"`), `en-US missing ${key}`);
}

console.log("backup migration UI smoke ok");
