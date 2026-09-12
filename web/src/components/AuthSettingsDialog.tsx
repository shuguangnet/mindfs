import React, { useEffect, useState } from "react";
import { Alert, Button, Input, Modal, Switch, Typography } from "antd";
import {
  AuthError,
  fetchAuthSettings,
  saveAuthSettings,
  type AuthSettings,
} from "../services/auth";
import { useI18n } from "../i18n";

const { Text } = Typography;

type AuthSettingsDialogProps = {
  open: boolean;
  onClose: () => void;
  onSaved?: (settings: AuthSettings) => void;
};

export function AuthSettingsDialog({ open, onClose, onSaved }: AuthSettingsDialogProps) {
  const { t } = useI18n();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [username, setUsername] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [hasPassword, setHasPassword] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError("");
    setNewPassword("");
    setCurrentPassword("");
    void fetchAuthSettings()
      .then((settings) => {
        if (cancelled) {
          return;
        }
        setEnabled(settings.enabled);
        setUsername(settings.username);
        setHasPassword(settings.has_password);
      })
      .catch(() => {
        if (!cancelled) {
          setError(t("auth.settings.errorGeneric"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, t]);

  const describeError = (err: unknown): string => {
    if (err instanceof AuthError) {
      if (err.code === "auth_password_not_configured") {
        return t("auth.settings.errorPasswordRequired");
      }
      if (err.status === 401 || err.code === "invalid_credentials") {
        return t("auth.settings.errorCurrent");
      }
    }
    return t("auth.settings.errorGeneric");
  };

  const save = async () => {
    if (saving) {
      return;
    }
    setSaving(true);
    setError("");
    try {
      const settings = await saveAuthSettings({
        enabled,
        username: username.trim(),
        new_password: newPassword ? newPassword : undefined,
        current_password: currentPassword ? currentPassword : undefined,
      });
      onSaved?.(settings);
      onClose();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      title={t("auth.settings.title")}
      onCancel={onClose}
      confirmLoading={saving}
      onOk={() => void save()}
      okText={saving ? t("auth.settings.saving") : t("auth.settings.save")}
      cancelText={t("common.cancel")}
      destroyOnClose
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <label style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Switch checked={enabled} onChange={setEnabled} disabled={loading || saving} />
          <span style={{ display: "flex", flexDirection: "column" }}>
            <span>{t("auth.settings.enabled")}</span>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t("auth.settings.enableHint")}
            </Text>
          </span>
        </label>
        <div>
          <div style={{ marginBottom: 6 }}>{t("auth.settings.username")}</div>
          <Input
            value={username}
            disabled={loading || saving}
            onChange={(event) => setUsername(event.target.value)}
          />
        </div>
        <div>
          <div style={{ marginBottom: 6 }}>{t("auth.settings.newPassword")}</div>
          <Input.Password
            value={newPassword}
            disabled={loading || saving}
            autoComplete="new-password"
            placeholder={t("auth.settings.newPasswordHint")}
            onChange={(event) => setNewPassword(event.target.value)}
          />
        </div>
        {hasPassword ? (
          <div>
            <div style={{ marginBottom: 6 }}>{t("auth.settings.currentPassword")}</div>
            <Input.Password
              value={currentPassword}
              disabled={loading || saving}
              autoComplete="current-password"
              placeholder={t("auth.settings.currentPasswordHint")}
              onChange={(event) => setCurrentPassword(event.target.value)}
            />
          </div>
        ) : null}
        {error ? <Alert type="error" showIcon message={error} /> : null}
      </div>
    </Modal>
  );
}

export default AuthSettingsDialog;
