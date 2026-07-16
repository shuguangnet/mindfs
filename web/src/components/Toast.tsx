import React, { useState, useEffect, useCallback } from "react";
import { Alert, Button, Space } from "antd";
import { errorService, type AppError } from "../services/error";
import { useI18n } from "../i18n";

type ToastItem = {
  id: string;
  error: AppError;
  expiresAt: number;
};

export function ToastContainer(): React.ReactElement {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  // Subscribe to errors
  useEffect(() => {
    const unsubscribe = errorService.subscribe((error) => {
      // Only show non-fatal errors as toasts
      if (error.severity === "fatal") return;

      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const duration = error.severity === "error" ? 5000 : 3000;

      setToasts((prev) => [
        ...prev,
        {
          id,
          error,
          expiresAt: Date.now() + duration,
        },
      ]);
    });

    return unsubscribe;
  }, []);

  // Auto-remove expired toasts
  useEffect(() => {
    if (toasts.length === 0) return;

    const timer = setInterval(() => {
      const now = Date.now();
      setToasts((prev) => prev.filter((t) => t.expiresAt > now));
    }, 500);

    return () => clearInterval(timer);
  }, [toasts.length]);

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const handleRetry = useCallback(async (toast: ToastItem) => {
    if (toast.error.retryAction) {
      removeToast(toast.id);
      try {
        await toast.error.retryAction();
      } catch (e) {
        console.error("Retry failed:", e);
      }
    }
  }, [removeToast]);

  if (toasts.length === 0) return <></>;

  return (
    <div
      style={{
        position: "fixed",
        bottom: "24px",
        left: "50%",
        transform: "translateX(-50%)",
        display: "flex",
        flexDirection: "column",
        gap: "8px",
        zIndex: 2100,
        maxWidth: "640px",
        width: "100%",
        padding: "0 16px",
      }}
    >
      {toasts.map((toast) => (
        <Toast
          key={toast.id}
          error={toast.error}
          onClose={() => removeToast(toast.id)}
          onRetry={toast.error.recoverable ? () => handleRetry(toast) : undefined}
        />
      ))}
    </div>
  );
}

type ToastProps = {
  error: AppError;
  onClose: () => void;
  onRetry?: () => void;
};

function Toast({ error, onClose, onRetry }: ToastProps): React.ReactElement {
  const { t } = useI18n();
  const type =
    error.severity === "error"
      ? "error"
      : error.severity === "warning"
        ? "warning"
        : "info";

  const message = error.usesDefaultMessage && error.messageKey ? t(error.messageKey) : error.message;

  return (
    <Alert
      type={type}
      showIcon
      closable
      onClose={onClose}
      message={message}
      description={error.code || undefined}
      action={
        onRetry ? (
          <Space size={8}>
            <Button size="small" onClick={onRetry}>
              重试
            </Button>
          </Space>
        ) : undefined
      }
      style={{
        borderRadius: "8px",
        boxShadow: "0 12px 32px rgba(15, 23, 42, 0.16)",
        animation: "toastSlideIn 0.2s ease-out",
        overflowWrap: "anywhere",
      }}
    />
  );
}
