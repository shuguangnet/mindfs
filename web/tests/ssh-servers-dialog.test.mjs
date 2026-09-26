import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const dialog = readFileSync(
  new URL("../src/components/SSHServersDialog.tsx", import.meta.url),
  "utf8",
);
const service = readFileSync(
  new URL("../src/services/sshServers.ts", import.meta.url),
  "utf8",
);
const fileTree = readFileSync(
  new URL("../src/components/FileTree.tsx", import.meta.url),
  "utf8",
);
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
  "/api/ssh-servers",
  "/fs",
  "/test",
  "/deploy-key",
  "/import/preview",
  "/import/apply",
  "/export",
]) {
  assert.ok(service.includes(endpoint), `service must call ${endpoint}`);
}

// Dialog supports the three auth modes with quick-reuse pickers.
assert.match(dialog, /\["password",\s*"key_path",\s*"key_inline"\]/, "auth mode switcher must exist");
assert.ok(dialog.includes("reuse_password_from"), "password quick reuse must exist");
assert.ok(dialog.includes("reuse_key_from"), "managed key quick reuse must exist");
assert.ok(dialog.includes("reuse.key_paths"), "key path quick reuse must exist");

// Key path supports a server-side file tree picker, not only manual input.
const picker = readFileSync(
  new URL("../src/components/SSHKeyFilePicker.tsx", import.meta.url),
  "utf8",
);
assert.ok(service.includes("browseSSHKeyFS"), "service must expose the fs browse call");
assert.ok(dialog.includes("SSHKeyFilePicker"), "dialog must render the key file picker");
assert.ok(dialog.includes('t("sshServers.fsBrowse")'), "key path row must have a browse button");
assert.ok(picker.includes("looks_like_key"), "picker must highlight likely private keys");
assert.ok(picker.includes("listing.parent"), "picker must support navigating to the parent directory");

// Secrets never render back into the form after load.
assert.ok(!dialog.includes("has_password ? form"), "no conditional secret echo");

// Import preview flow with per-conflict resolution.
assert.ok(dialog.includes("needs_passphrase"), "import passphrase flow must exist");
assert.ok(dialog.includes('value="suffix"'), "suffix resolution option must exist");
assert.ok(dialog.includes('value="overwrite"'), "overwrite resolution option must exist");
assert.ok(dialog.includes('value="skip"'), "skip resolution option must exist");

// Export downloads a file and honors the passphrase/no-secrets modes.
assert.ok(service.includes("downloadSSHExportFile"), "export download helper must exist");
assert.match(dialog, /exportPassphrase \? "encrypted" : "no-secrets"/, "export mode follows passphrase presence");

// Sidebar wiring in FileTree.
assert.ok(fileTree.includes("SSHServersDialog"), "FileTree must render SSHServersDialog");
assert.ok(fileTree.includes("setSSHServersOpen(true)"), "FileTree menu must open the SSH dialog");

// Localized text in both locales without fallback to raw keys.
const key = /"sshServers\.title":\s*"[^"]+"/;
assert.match(zhCN, key, "zh-CN must define sshServers.title");
assert.match(enUS, key, "en-US must define sshServers.title");
assert.equal(
  (zhCN.match(/"sshServers\./g) || []).length,
  (enUS.match(/"sshServers\./g) || []).length,
  "zh-CN and en-US must define the same sshServers keys",
);
