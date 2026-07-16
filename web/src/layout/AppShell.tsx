import React, { useState, useEffect } from "react";
import { Button, Drawer, Layout, Tooltip } from "antd";
import { useI18n } from "../i18n";

const { Content, Footer, Sider } = Layout;

type AppShellProps = {
  sidebar: React.ReactNode;
  main: React.ReactNode;
  rightSidebar?: React.ReactNode;
  footer: React.ReactNode;
  drawer?: React.ReactNode;
  leftOpen?: boolean;
  rightOpen?: boolean;
  onCloseLeft?: () => void;
  onCloseRight?: () => void;
  onOpenLeft?: () => void;
  onOpenRight?: () => void;
  sidebarsSwapped?: boolean;
};

const MOBILE_BREAKPOINT = 768;
const TABLET_BREAKPOINT = 1024;

function useResponsive() {
  const [isMobile, setIsMobile] = useState(false);
  const [isTablet, setIsTablet] = useState(false);
  useEffect(() => {
    const checkSize = () => {
      const width = window.innerWidth;
      setIsMobile(width < MOBILE_BREAKPOINT);
      setIsTablet(width >= MOBILE_BREAKPOINT && width < TABLET_BREAKPOINT);
    };
    checkSize();
    window.addEventListener("resize", checkSize);
    return () => window.removeEventListener("resize", checkSize);
  }, []);
  return { isMobile, isTablet };
}

const sidebarStyle: React.CSSProperties = {
  gridArea: "sidebar",
  borderRight: "1px solid var(--border-color)",
  overflow: "auto",
  background: "var(--mindfs-topbar-bg, var(--sidebar-bg))",
  display: "flex",
  flexDirection: "column",
  position: "relative",
  zIndex: 10,
};

const mainStyle: React.CSSProperties = {
  gridArea: "main",
  width: "100%",
  minWidth: 0,
  overflow: "hidden",
  padding: "0",
  background: "var(--mindfs-topbar-bg, var(--mobile-overlay-bg, var(--content-bg)))",
  display: "flex",
  flexDirection: "column",
  minHeight: 0,
  position: "relative",
  zIndex: 1,
  contain: "paint",
};

const rightStyle: React.CSSProperties = {
  gridArea: "right",
  borderLeft: "1px solid var(--border-color)",
  overflow: "auto",
  background: "var(--mindfs-topbar-bg, var(--sidebar-bg))",
  display: "flex",
  flexDirection: "column",
  position: "relative",
  zIndex: 10,
};

const footerStyle: React.CSSProperties = {
  gridArea: "footer",
  borderTop: "none",
  padding: "0",
  display: "flex",
  alignItems: "flex-end",
  justifyContent: "center",
  background: "var(--mindfs-topbar-bg, var(--mobile-overlay-bg, var(--content-bg)))",
  zIndex: 100,
  minWidth: 0,
};

export function AppShell({
  sidebar,
  main,
  rightSidebar,
  footer,
  drawer,
  leftOpen = true,
  rightOpen = true,
  onCloseLeft,
  onCloseRight,
  onOpenLeft,
  onOpenRight,
  sidebarsSwapped = false,
}: AppShellProps) {
  const { t } = useI18n();
  const { isMobile, isTablet } = useResponsive();

  const sidebarWidth = isMobile ? "0px" : (isTablet ? "200px" : "260px");
  const rightWidth = isMobile ? "0px" : (rightSidebar ? (isTablet ? "240px" : "280px") : "0px");
  const mobileHeight = "var(--mindfs-viewport-height, 100dvh)";
  const physicalLeftOpen = sidebarsSwapped ? rightOpen : leftOpen;
  const physicalRightOpen = sidebarsSwapped ? leftOpen : rightOpen;
  const physicalLeftWidth = sidebarsSwapped ? rightWidth : sidebarWidth;
  const physicalRightWidth = sidebarsSwapped ? sidebarWidth : rightWidth;
  const physicalLeftContent = sidebarsSwapped ? rightSidebar : sidebar;
  const physicalRightContent = sidebarsSwapped ? sidebar : rightSidebar;
  const physicalLeftClose = sidebarsSwapped ? onCloseRight : onCloseLeft;
  const physicalLeftOpenHandler = sidebarsSwapped ? onOpenRight : onOpenLeft;
  const physicalRightClose = sidebarsSwapped ? onCloseLeft : onCloseRight;
  const physicalRightOpenHandler = sidebarsSwapped ? onOpenLeft : onOpenRight;
  const physicalLeftLabel = sidebarsSwapped ? t("sidebar.session") : t("sidebar.file");
  const physicalRightLabel = sidebarsSwapped ? t("sidebar.file") : t("sidebar.session");

  const shellStyle: React.CSSProperties & {
    "--mindfs-actionbar-bottom-padding"?: string;
  } = {
    display: isMobile ? "flex" : "grid",
    flexDirection: isMobile ? "column" : undefined,
    gridTemplateColumns: isMobile ? undefined : `${physicalLeftOpen ? physicalLeftWidth : "0px"} 1fr ${physicalRightOpen ? physicalRightWidth : "0px"}`,
    gridTemplateRows: isMobile ? undefined : "1fr auto",
    gridTemplateAreas: isMobile ? undefined : `"sidebar main right" "sidebar footer right"`,
    minHeight: isMobile ? mobileHeight : "100vh",
    height: isMobile ? mobileHeight : "100dvh",
    background: isMobile
      ? "var(--mindfs-topbar-bg, var(--mindfs-system-bar-bg, var(--mobile-overlay-bg, var(--content-bg))))"
      : "var(--bg-gradient-composite, var(--bg-gradient-start, #f3f4f6))",
    color: "var(--text-primary)",
    position: "relative",
    width: isMobile ? "100%" : undefined,
    maxWidth: isMobile ? "100%" : undefined,
    paddingTop: isMobile ? "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))" : undefined,
    overflow: "hidden",
    isolation: "isolate",
    boxSizing: "border-box",
    transition: "grid-template-columns 0.3s cubic-bezier(0.4, 0, 0.2, 1)",
    "--mindfs-actionbar-bottom-padding": "calc(var(--mindfs-safe-area-bottom) + 12px)",
  };

  const mobileDrawerContentStyle = (side: 'left' | 'right'): React.CSSProperties => ({
    top: "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))",
    bottom: 0,
    background: "var(--mindfs-topbar-bg, var(--mobile-sidebar-bg, var(--sidebar-bg)))",
    boxShadow: side === 'left' ? "4px 0 24px rgba(0,0,0,0.15)" : "-4px 0 24px rgba(0,0,0,0.15)",
    display: "flex",
    flexDirection: "column",
    overflow: "hidden",
    borderTopRightRadius: side === 'left' ? "14px" : undefined,
    borderBottomRightRadius: side === 'left' ? "14px" : undefined,
    borderTopLeftRadius: side === 'right' ? "14px" : undefined,
    borderBottomLeftRadius: side === 'right' ? "14px" : undefined,
    backfaceVisibility: "hidden",
  });

  const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    background: "rgba(0,0,0,0.3)",
    zIndex: 1500,
    opacity: (isMobile && (leftOpen || rightOpen)) ? 1 : 0,
    pointerEvents: (isMobile && (leftOpen || rightOpen)) ? "auto" : "none",
    transition: "opacity 0.18s ease",
    willChange: "opacity",
    backfaceVisibility: "hidden",
    transform: "translateZ(0)",
  };

  const mobileFooterStyle: React.CSSProperties = {
    ...footerStyle,
    flexShrink: 0,
  };

  return (
    <Layout className="mindfs-enterprise-shell" style={shellStyle}>
      {isMobile && <div style={overlayStyle} onClick={() => { onCloseLeft?.(); onCloseRight?.(); }} />}

      {isMobile ? (
        <>
          <Drawer
            open={physicalLeftOpen && !!physicalLeftContent}
            onClose={physicalLeftClose}
            placement="left"
            size="75vw"
            closable={false}
            mask={false}
            styles={{
              wrapper: {
                top: "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))",
                bottom: 0,
              },
              body: {
                padding: 0,
                display: "flex",
                flexDirection: "column",
                overflow: "hidden",
              },
              section: mobileDrawerContentStyle("left"),
            }}
          >
            {physicalLeftContent}
          </Drawer>
          <Drawer
            open={physicalRightOpen && !!physicalRightContent}
            onClose={physicalRightClose}
            placement="right"
            size="75vw"
            closable={false}
            mask={false}
            styles={{
              wrapper: {
                top: "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))",
                bottom: 0,
              },
              body: {
                padding: 0,
                display: "flex",
                flexDirection: "column",
                overflow: "hidden",
              },
              section: mobileDrawerContentStyle("right"),
            }}
          >
            {physicalRightContent}
          </Drawer>
        </>
      ) : physicalLeftContent ? (
        <Sider
          width={physicalLeftOpen ? physicalLeftWidth : 0}
          style={{
            ...sidebarStyle,
            overflow: physicalLeftOpen ? "auto" : "hidden",
            pointerEvents: physicalLeftOpen ? "auto" : "none",
          }}
        >
          {physicalLeftContent}
        </Sider>
      ) : null}

      <Content
        style={
          isMobile
            ? {
                ...mainStyle,
                flex: 1,
                minHeight: 0,
                minWidth: 0,
              }
            : mainStyle
        }
      >
        {main}
        {/* 将抽屉层放入主视图内部，确保绝对定位时能精准对齐主视图宽度 */}
        {drawer}
      </Content>

      {!isMobile && physicalRightContent ? (
        <Sider
          width={physicalRightOpen ? physicalRightWidth : 0}
          style={{
            ...rightStyle,
            overflow: physicalRightOpen ? "auto" : "hidden",
            pointerEvents: physicalRightOpen ? "auto" : "none",
          }}
        >
          {physicalRightContent}
        </Sider>
      ) : null}

      {!isMobile ? (
        <>
          <Tooltip
            title={physicalLeftOpen ? `收起${physicalLeftLabel}` : `展开${physicalLeftLabel}`}
            placement="right"
          >
            <Button
              type="text"
              htmlType="button"
              className={`mindfs-sidebar-resize-rail mindfs-sidebar-resize-rail--left${physicalLeftOpen ? " is-open" : " is-closed"}`}
              onClick={physicalLeftOpen ? physicalLeftClose : physicalLeftOpenHandler}
              aria-label={physicalLeftOpen ? `收起${physicalLeftLabel}` : `展开${physicalLeftLabel}`}
              title={physicalLeftOpen ? `收起${physicalLeftLabel}` : `展开${physicalLeftLabel}`}
              style={{
                left: physicalLeftOpen ? `calc(${physicalLeftWidth} - 6px)` : 0,
                cursor: physicalLeftOpen ? "w-resize" : "e-resize",
              }}
            />
          </Tooltip>
          {physicalRightContent ? (
            <Tooltip
              title={physicalRightOpen ? `收起${physicalRightLabel}` : `展开${physicalRightLabel}`}
              placement="left"
            >
              <Button
                type="text"
                htmlType="button"
                className={`mindfs-sidebar-resize-rail mindfs-sidebar-resize-rail--right${physicalRightOpen ? " is-open" : " is-closed"}`}
                onClick={physicalRightOpen ? physicalRightClose : physicalRightOpenHandler}
                aria-label={physicalRightOpen ? `收起${physicalRightLabel}` : `展开${physicalRightLabel}`}
                title={physicalRightOpen ? `收起${physicalRightLabel}` : `展开${physicalRightLabel}`}
                style={{
                  right: physicalRightOpen ? `calc(${physicalRightWidth} - 6px)` : 0,
                  cursor: physicalRightOpen ? "e-resize" : "w-resize",
                }}
              />
            </Tooltip>
          ) : null}
        </>
      ) : null}

      <Footer
        style={
          isMobile
            ? mobileFooterStyle
            : footerStyle
        }
      >
        {footer}
      </Footer>
    </Layout>
  );
}
