import React from "react";
import { Layout, Typography } from "antd";

const { Content, Header } = Layout;
const { Text } = Typography;

type RightSidebarProps = {
  children?: React.ReactNode;
};

export function RightSidebar({ children }: RightSidebarProps) {
  return (
    <Layout style={{ flex: 1, minHeight: 0, background: "transparent" }}>
      <Header
        style={{
          height: "36px",
          padding: "0 16px",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          borderBottom: "1px solid var(--border-color)",
          background: "var(--mindfs-topbar-bg, transparent)",
          position: "sticky",
          top: 0,
          zIndex: 2,
          backdropFilter: "blur(8px)",
          boxSizing: "border-box",
        }}
      >
        <Text
          style={{
            fontSize: "11px",
            fontWeight: 600,
            color: "var(--text-secondary)",
            textTransform: "uppercase",
          }}
        >
          会话
        </Text>
      </Header>
      <Content style={{ flex: 1, minHeight: 0, overflow: "auto", padding: "12px 12px 16px" }}>
        {children}
      </Content>
    </Layout>
  );
}
