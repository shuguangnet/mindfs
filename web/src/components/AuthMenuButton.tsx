import React, { useEffect, useState } from "react";
import { Dropdown, Tooltip } from "antd";
import {
  LogoutOutlined,
  SettingOutlined,
  UserOutlined,
} from "@ant-design/icons";
import {
  fetchAuthStatus,
  logoutAuth,
  notifyUnauthenticated,
  type AuthStatus,
} from "../services/auth";
import { AuthSettingsDialog } from "./AuthSettingsDialog";
import { useI18n } from "../i18n";

/**
 * Account menu shown in the top bar. It exposes the auth settings dialog (used
 * to enable/disable the login gate and change credentials) and, when the gate
 * is on, a sign-out action.
 */
export function AuthMenuButton() {
  const { t } = useI18n();
  const [status, setStatus] = useState<AuthStatus | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void fetchAuthStatus()
      .then((next) => {
        if (!cancelled) {
          setStatus(next);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setStatus({ enabled: false, authenticated: true });
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleSignOut = async () => {
    await logoutAuth();
    notifyUnauthenticated();
  };

  const menuItems = [
    {
      key: "settings",
      icon: <SettingOutlined />,
      label: t("auth.menu.settings"),
    },
    ...(status?.enabled
      ? [
          {
            key: "logout",
            icon: <LogoutOutlined />,
            label: t("auth.menu.logout"),
            danger: true,
          },
        ]
      : []),
  ];

  return (
    <>
      <Dropdown
        trigger={["click"]}
        placement="bottomRight"
        menu={{
          items: menuItems,
          onClick: ({ key }) => {
            if (key === "settings") {
              setSettingsOpen(true);
            } else if (key === "logout") {
              void handleSignOut();
            }
          },
        }}
      >
        <Tooltip title={t("auth.menu.account")} placement="left">
          <button
            type="button"
            aria-label={t("auth.menu.account")}
            style={{
              width: "28px",
              height: "28px",
              borderRadius: "8px",
              border: "none",
              background: status?.enabled ? "rgba(37, 99, 235, 0.12)" : "transparent",
              color: "var(--text-secondary)",
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              cursor: "pointer",
              outline: "none",
              flexShrink: 0,
            }}
          >
            <UserOutlined style={{ fontSize: 15 }} />
          </button>
        </Tooltip>
      </Dropdown>
      <AuthSettingsDialog
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        onSaved={(settings) => setStatus((prev) => ({ ...(prev ?? { authenticated: true }), ...settings }))}
      />
    </>
  );
}

export default AuthMenuButton;
