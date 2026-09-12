import React, { useState } from "react";
import { Alert, Button, Card, Checkbox, Input, Typography } from "antd";
import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { AuthError, loginAuth } from "../services/auth";
import { useI18n } from "../i18n";

const { Title, Text } = Typography;

const BG =
  "radial-gradient(circle at top left, rgba(91, 125, 184, 0.10), transparent 26%), radial-gradient(circle at right 18%, rgba(148, 163, 184, 0.16), transparent 28%), linear-gradient(180deg, #f8fafc 0%, #edf2f7 100%)";

type AuthLoginProps = {
  onSuccess: () => void;
};

export function AuthLogin({ onSuccess }: AuthLoginProps) {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [remember, setRemember] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const describeError = (err: unknown): string => {
    if (err instanceof AuthError) {
      if (err.status === 429 || err.code === "too_many_attempts") {
        return t("auth.login.errorLocked");
      }
      if (err.status === 401 || err.code === "invalid_credentials") {
        return t("auth.login.errorInvalid");
      }
    }
    return t("auth.login.errorGeneric");
  };

  const submit = async () => {
    if (busy) {
      return;
    }
    if (!username.trim() || !password) {
      setError(t("auth.login.errorInvalid"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      await loginAuth(username.trim(), password, remember);
      onSuccess();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      style={{
        minHeight: "100dvh",
        background: "var(--mindfs-system-bar-bg, #f8fafc)",
        color: "var(--text-primary, #0f172a)",
        padding:
          "calc(var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px)) + 20px) 16px calc(var(--mindfs-safe-area-bottom, env(safe-area-inset-bottom, 0px)) + 24px)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        position: "relative",
      }}
    >
      <div
        style={{
          position: "fixed",
          inset: 0,
          background: `var(--mindfs-launcher-bg, ${BG})`,
          pointerEvents: "none",
          zIndex: 0,
        }}
      />
      <Card
        style={{
          position: "relative",
          zIndex: 1,
          width: "100%",
          maxWidth: 400,
          borderRadius: 16,
          boxShadow: "0 24px 60px rgba(15, 23, 42, 0.16)",
        }}
      >
        <div style={{ display: "flex", flexDirection: "column", gap: 6, marginBottom: 20 }}>
          <img
            src="/favicon.svg"
            alt=""
            width={40}
            height={40}
            style={{ display: "block", marginBottom: 4 }}
            onError={(event) => {
              (event.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
          <Title level={3} style={{ margin: 0 }}>
            {t("auth.login.title")}
          </Title>
          <Text type="secondary">{t("auth.login.subtitle")}</Text>
        </div>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
          style={{ display: "flex", flexDirection: "column", gap: 12 }}
        >
          <Input
            size="large"
            autoFocus
            autoComplete="username"
            prefix={<UserOutlined />}
            placeholder={t("auth.login.username")}
            value={username}
            disabled={busy}
            onChange={(event) => setUsername(event.target.value)}
          />
          <Input.Password
            size="large"
            autoComplete="current-password"
            prefix={<LockOutlined />}
            placeholder={t("auth.login.password")}
            value={password}
            disabled={busy}
            onChange={(event) => setPassword(event.target.value)}
          />
          <Checkbox
            checked={remember}
            disabled={busy}
            onChange={(event) => setRemember(event.target.checked)}
          >
            {t("auth.login.remember")}
          </Checkbox>
          {error ? <Alert type="error" showIcon message={error} /> : null}
          <Button
            type="primary"
            size="large"
            htmlType="submit"
            loading={busy}
            block
          >
            {busy ? t("auth.login.submitting") : t("auth.login.submit")}
          </Button>
        </form>
      </Card>
    </div>
  );
}

export default AuthLogin;
