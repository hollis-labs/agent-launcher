import { Component } from "react";

// Without this, an uncaught render error anywhere below it unmounts the
// entire React tree and leaves the native window showing nothing but its
// own chrome — no header, no tabs, nothing to click. That is exactly how the
// Tree()-returns-null-nodes bug (fixed in internal/manager) presented: a
// permanently blank manager window with no indication anything had gone
// wrong. React error boundaries only catch render/lifecycle errors in class
// components, which is why this exists as one rather than a hook.
//
// This is deliberately generic — it is not a fix for that one bug, it is the
// backstop for the whole class: whatever the next render-time exception
// turns out to be, the window shows a message instead of going silently
// blank.
export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error("Tachyon: uncaught render error", error, info?.componentStack);
  }

  render() {
    if (this.state.error) {
      const message = this.state.error?.message ?? String(this.state.error);
      return (
        <div style={boundaryStyle}>
          <p style={{ marginBottom: 10 }}>Tachyon hit an error rendering this window.</p>
          <pre style={preStyle}>{message}</pre>
          <p style={{ marginTop: 14, opacity: 0.7 }}>
            Nothing was written to disk by this. Reopen the window from the tray to retry.
          </p>
        </div>
      );
    }
    return this.props.children;
  }
}

const boundaryStyle = {
  padding: 20,
  fontFamily: "ui-monospace, SF Mono, Menlo, monospace",
  fontSize: 13,
  color: "#e06c75",
  lineHeight: 1.6,
};

const preStyle = {
  whiteSpace: "pre-wrap",
  wordBreak: "break-word",
  background: "#1b1e24",
  border: "1px solid #262a31",
  borderRadius: 6,
  padding: 12,
  color: "#e0e6ea",
};
