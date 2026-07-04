import React, { useEffect, useMemo, useState } from "react";
import { App as AntApp, ConfigProvider, theme } from "antd";
import type { ThemeConfig } from "antd";
import zhCN from "antd/locale/zh_CN";

type AntdProviderProps = {
  children: React.ReactNode;
};

const enterpriseTokens: NonNullable<ThemeConfig["token"]> = {
  colorPrimary: "#2563eb",
  colorInfo: "#2563eb",
  colorSuccess: "#16a34a",
  colorWarning: "#d97706",
  colorError: "#dc2626",
  colorTextBase: "#0f172a",
  colorBgBase: "#ffffff",
  borderRadius: 8,
  borderRadiusLG: 8,
  borderRadiusSM: 6,
  controlHeight: 34,
  controlHeightLG: 40,
  controlHeightSM: 28,
  fontFamily:
    "Inter, -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, Helvetica, Arial, sans-serif",
  boxShadow:
    "0 12px 32px rgba(15, 23, 42, 0.10)",
  boxShadowSecondary:
    "0 8px 24px rgba(15, 23, 42, 0.08)",
};

function getResolvedTheme(): string {
  if (typeof document === "undefined") {
    return "light";
  }
  const explicit = document.documentElement.dataset.theme;
  if (explicit) {
    return explicit;
  }
  if (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
  ) {
    return "dark";
  }
  return "light";
}

export function MindFSAntdProvider({ children }: AntdProviderProps): React.ReactElement {
  const [resolvedTheme, setResolvedTheme] = useState(getResolvedTheme);

  useEffect(() => {
    if (typeof document === "undefined") {
      return;
    }

    const updateTheme = () => setResolvedTheme(getResolvedTheme());
    const observer = new MutationObserver(updateTheme);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });

    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener?.("change", updateTheme);
    return () => {
      observer.disconnect();
      media?.removeEventListener?.("change", updateTheme);
    };
  }, []);

  const config = useMemo<ThemeConfig>(() => {
    const isDark = resolvedTheme === "dark";
    const themedTokens: ThemeConfig["token"] = {
      ...enterpriseTokens,
      colorPrimary:
        resolvedTheme === "meadow"
          ? "#2d5a32"
          : resolvedTheme === "moss"
            ? "#415b3f"
            : enterpriseTokens.colorPrimary,
      colorInfo:
        resolvedTheme === "meadow"
          ? "#2d5a32"
          : resolvedTheme === "moss"
            ? "#415b3f"
            : enterpriseTokens.colorInfo,
      colorBgBase: isDark ? "#020617" : "#ffffff",
      colorTextBase: isDark ? "#f8fafc" : "#0f172a",
    };

    return {
      algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: themedTokens,
      components: {
        Layout: {
          bodyBg: "transparent",
          headerBg: "var(--mindfs-topbar-bg, #ffffff)",
          siderBg: "var(--sidebar-bg, #ffffff)",
          footerBg: "var(--content-bg, #ffffff)",
        },
        Button: {
          borderRadius: 7,
          controlHeight: 32,
        },
        Card: {
          borderRadiusLG: 8,
          paddingLG: 16,
        },
        Modal: {
          borderRadiusLG: 10,
        },
        Drawer: {
          colorBgElevated: "var(--mobile-sidebar-bg, #ffffff)",
        },
        Table: {
          borderColor: "var(--border-color)",
          headerBg: "var(--panel-bg)",
        },
      },
    };
  }, [resolvedTheme]);

  return (
    <ConfigProvider locale={zhCN} theme={config}>
      <AntApp component="div">{children}</AntApp>
    </ConfigProvider>
  );
}
