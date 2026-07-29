import {
  Component,
  useEffect,
  useId,
  useRef,
  type ButtonHTMLAttributes,
  type ErrorInfo,
  type ReactNode,
} from "react";

export type IconName =
  | "activity"
  | "audit"
  | "check"
  | "close"
  | "dashboard"
  | "devices"
  | "download"
  | "gauge"
  | "logout"
  | "plus"
  | "refresh"
  | "server"
  | "shield"
  | "traffic"
  | "upload";

const iconPaths: Record<IconName, ReactNode> = {
  activity: <><path d="M3 12h4l2-7 4 14 2-7h6" /></>,
  audit: <><path d="M6 3h12v18H6z" /><path d="M9 8h6M9 12h6M9 16h4" /></>,
  check: <path d="m5 12 4 4L19 6" />,
  close: <><path d="m6 6 12 12" /><path d="M18 6 6 18" /></>,
  dashboard: <><rect x="3" y="3" width="7" height="7" rx="2" /><rect x="14" y="3" width="7" height="7" rx="2" /><rect x="3" y="14" width="7" height="7" rx="2" /><rect x="14" y="14" width="7" height="7" rx="2" /></>,
  devices: <><rect x="3" y="4" width="13" height="16" rx="2" /><path d="M8 17h3M18 8h3v9a2 2 0 0 1-2 2h-1" /></>,
  download: <><path d="M12 3v12" /><path d="m7 10 5 5 5-5" /><path d="M5 21h14" /></>,
  gauge: <><path d="M4.9 19a9 9 0 1 1 14.2 0" /><path d="m12 13 4-4" /><path d="M12 19h.01" /></>,
  logout: <><path d="M10 17l5-5-5-5M15 12H3" /><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" /></>,
  plus: <><path d="M12 5v14M5 12h14" /></>,
  refresh: <><path d="M20 11a8 8 0 1 0-2.34 5.66" /><path d="M20 4v7h-7" /></>,
  server: <><rect x="3" y="3" width="18" height="7" rx="2" /><rect x="3" y="14" width="18" height="7" rx="2" /><path d="M7 6.5h.01M7 17.5h.01M11 6.5h6M11 17.5h6" /></>,
  shield: <><path d="M12 22s8-4 8-11V5l-8-3-8 3v6c0 7 8 11 8 11Z" /><path d="m9 12 2 2 4-5" /></>,
  traffic: <><path d="M7 3v18M3 7l4-4 4 4M17 21V3M13 17l4 4 4-4" /></>,
  upload: <><path d="M12 21V9" /><path d="m7 14 5-5 5 5" /><path d="M5 3h14" /></>,
};

export function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  return (
    <svg
      aria-hidden="true"
      className="ui-icon"
      fill="none"
      height={size}
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
      viewBox="0 0 24 24"
      width={size}
    >
      {iconPaths[name]}
    </svg>
  );
}

export function Button({
  children,
  icon,
  loading = false,
  variant = "primary",
  className = "",
  type = "button",
  disabled,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: IconName;
  loading?: boolean;
  variant?: "primary" | "secondary" | "ghost" | "danger";
}) {
  return (
    <button
      className={`ui-button ui-button-${variant} ${className}`.trim()}
      disabled={disabled || loading}
      type={type}
      {...props}
    >
      {loading ? <Spinner /> : icon ? <Icon name={icon} size={17} /> : null}
      <span>{children}</span>
    </button>
  );
}

export function Panel({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <section className={`ui-panel ${className}`.trim()}>{children}</section>;
}

export function Status({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "online" | "warning" | "danger";
}) {
  return (
    <span className={`ui-status ui-status-${tone}`}>
      {tone === "online" && <i aria-hidden="true" />}
      {children}
    </span>
  );
}

export function Tabs<T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: Array<{ value: T; label: string }>;
  value: T;
  onChange: (value: T) => void;
  label: string;
}) {
  return (
    <div aria-label={label} className="ui-tabs" role="tablist">
      {items.map((item) => (
        <button
          aria-selected={item.value === value}
          className={item.value === value ? "active" : ""}
          key={item.value}
          onClick={() => onChange(item.value)}
          role="tab"
          type="button"
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="ui-empty">
      <span><Icon name="activity" /></span>
      <strong>{title}</strong>
      <p>{description}</p>
      {action}
    </div>
  );
}

export function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div className="ui-error" role="alert">
      <span><Icon name="shield" /></span>
      <div><strong>暂时无法显示</strong><p>{message}</p></div>
      {onRetry && <Button icon="refresh" onClick={onRetry} variant="secondary">重新加载</Button>}
    </div>
  );
}

export function Skeleton({ height = 180 }: { height?: number }) {
  return <div aria-label="正在加载" className="ui-skeleton" style={{ height }} />;
}

export function Spinner() {
  return <span aria-hidden="true" className="ui-spinner" />;
}

export function Dialog({
  open,
  title,
  description,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  description?: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    panelRef.current?.querySelector<HTMLElement>("input, select, button")?.focus();
    const close = (event: KeyboardEvent) => event.key === "Escape" && onClose();
    document.addEventListener("keydown", close);
    return () => {
      document.body.style.overflow = previous;
      document.removeEventListener("keydown", close);
    };
  }, [onClose, open]);
  if (!open) return null;
  return (
    <div className="ui-dialog-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div aria-labelledby={titleId} aria-modal="true" className="ui-dialog" ref={panelRef} role="dialog">
        <header>
          <div><h2 id={titleId}>{title}</h2>{description && <p>{description}</p>}</div>
          <button aria-label="关闭" className="ui-icon-button" onClick={onClose}><Icon name="close" /></button>
        </header>
        {children}
      </div>
    </div>
  );
}

export class ErrorBoundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("TNest VPN page failure", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <Panel>
          <ErrorState
            message="页面遇到异常，其他 VPN 服务不受影响。"
            onRetry={() => {
              this.setState({ error: null });
              window.location.reload();
            }}
          />
        </Panel>
      );
    }
    return this.props.children;
  }
}
