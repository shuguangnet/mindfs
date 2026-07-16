import React, { Component, type ReactNode } from "react";
import { Button, Card, Result } from "antd";

type ErrorBoundaryProps = {
  children: ReactNode;
  fallback?: ReactNode;
  onError?: (error: Error, errorInfo: React.ErrorInfo) => void;
  name?: string;
  labels?: {
    retry: string;
    unknownError: string;
    genericTitle: string;
    titleWithName: (name: string) => string;
  };
};

type ErrorBoundaryState = {
  hasError: boolean;
  error: Error | null;
};

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo): void {
    const { onError, name } = this.props;

    // Log error
    console.error(`[ErrorBoundary${name ? `:${name}` : ""}]`, error, errorInfo);

    // Call error handler
    onError?.(error, errorInfo);
  }

  handleRetry = (): void => {
    this.setState({ hasError: false, error: null });
  };

  render(): ReactNode {
    const { hasError, error } = this.state;
    const { children, fallback, name } = this.props;
    const labels = this.props.labels || {
      retry: translateNow("common.retry"),
      unknownError: translateNow("common.unknownError"),
      genericTitle: translateNow("error.boundary.genericTitle"),
      titleWithName: (value: string) => translateNow("error.boundary.title", { name: value }),
    };

    if (hasError) {
      if (fallback) {
        return fallback;
      }

      return (
        <Card
          bordered
          style={{
            margin: 20,
            background: "var(--panel-bg)",
            borderColor: "var(--panel-border)",
          }}
        >
          <Result
            status="error"
            title={name ? `${name} 出错了` : "出错了"}
            subTitle={error?.message || "发生了未知错误"}
            extra={[
              <Button key="retry" type="primary" onClick={this.handleRetry}>
                重试
              </Button>,
            ]}
          />
        </Card>
      );
    }

    return children;
  }
}

function LocalizedErrorBoundary(props: ErrorBoundaryProps): React.ReactElement {
  const { t } = useI18n();
  return (
    <ErrorBoundary
      {...props}
      labels={{
        retry: t("common.retry"),
        unknownError: t("common.unknownError"),
        genericTitle: t("error.boundary.genericTitle"),
        titleWithName: (name) => t("error.boundary.title", { name }),
      }}
    />
  );
}

// Specialized error boundaries
export function MainViewErrorBoundary({ children }: { children: ReactNode }): React.ReactElement {
  const { t } = useI18n();
  return (
    <LocalizedErrorBoundary
      name={t("error.boundary.mainView")}
      onError={(error) => {
        // Could send to audit log here
        console.error("[MainView Error]", error);
      }}
    >
      {children}
    </LocalizedErrorBoundary>
  );
}

export function DrawerPanelErrorBoundary({ children }: { children: ReactNode }): React.ReactElement {
  const { t } = useI18n();
  return (
    <LocalizedErrorBoundary
      name={t("error.boundary.drawer")}
      onError={(error) => {
        console.error("[DrawerPanel Error]", error);
      }}
    >
      {children}
    </LocalizedErrorBoundary>
  );
}
