import { useEffect, useRef, useState } from "react";
import type { Match } from "../types/proof";

interface Props {
  match: Match;
  position: { x: number; y: number };
  onApply: (value: string) => void;
  onDismiss: () => void;
}

const ISSUE_COLOR: Record<string, string> = {
  misspelling:   "#e74c3c",
  grammar:       "#e67e22",
  style:         "#3498db",
  typographical: "#9b59b6",
};

const ISSUE_LABEL: Record<string, string> = {
  misspelling:   "Spelling",
  grammar:       "Grammar",
  style:         "Style",
  typographical: "Punctuation",
};

export default function SuggestionPopup({ match, position, onApply, onDismiss }: Props) {
  const ref        = useRef<HTMLDivElement>(null);
  const listRef    = useRef<HTMLUListElement>(null);
  const [active, setActive] = useState(0);

  const suggestions = match.replacements.slice(0, 6);
  const color       = ISSUE_COLOR[match.rule.issueType] ?? "#888";
  const label       = ISSUE_LABEL[match.rule.issueType] ?? match.rule.category.name;

  // Auto-position: appear below word, flip up if near bottom
  const W = 280;
  const left = Math.min(Math.max(position.x, 8), window.innerWidth - W - 8);
  const flipUp = position.y + 280 > window.innerHeight;
  const top = flipUp ? position.y - 8 : position.y + 4;

  // Keyboard navigation
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") { onDismiss(); return; }
      if (suggestions.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((i) => (i + 1) % suggestions.length);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) => (i - 1 + suggestions.length) % suggestions.length);
      } else if (e.key === "Enter") {
        e.preventDefault();
        onApply(suggestions[active].value);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [active, suggestions, onApply, onDismiss]);

  // Scroll active item into view
  useEffect(() => {
    const el = listRef.current?.children[active] as HTMLElement | undefined;
    el?.scrollIntoView({ block: "nearest" });
  }, [active]);

  // Click outside → dismiss
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onDismiss();
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [onDismiss]);

  return (
    <div
      ref={ref}
      style={{
        position: "fixed",
        zIndex: 9999,
        width: W,
        background: "#1e1e1e",
        border: "1px solid #454545",
        borderRadius: 6,
        boxShadow: "0 4px 20px rgba(0,0,0,0.4)",
        fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
        fontSize: 13,
        color: "#cccccc",
        top: flipUp ? undefined : top,
        bottom: flipUp ? window.innerHeight - top : undefined,
        left,
        overflow: "hidden",
      }}
    >
      {/* Header — error message */}
      <div style={{
        padding: "8px 12px",
        borderBottom: "1px solid #333",
        display: "flex",
        alignItems: "flex-start",
        gap: 8,
      }}>
        <span style={{
          marginTop: 2,
          width: 8, height: 8, borderRadius: "50%",
          background: color, flexShrink: 0,
          display: "inline-block",
        }} />
        <div>
          <div style={{ fontSize: 11, color: "#888", marginBottom: 2, textTransform: "uppercase", letterSpacing: "0.5px" }}>
            {label}
          </div>
          <div style={{ color: "#d4d4d4", lineHeight: 1.4 }}>{match.message}</div>
        </div>
      </div>

      {/* Suggestions list */}
      {suggestions.length > 0 ? (
        <ul ref={listRef} style={{ margin: 0, padding: "4px 0", listStyle: "none", maxHeight: 220, overflowY: "auto" }}>
          {suggestions.map((r, i) => (
            <li
              key={r.value}
              onMouseEnter={() => setActive(i)}
              onClick={() => onApply(r.value)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "6px 12px",
                cursor: "pointer",
                background: i === active ? "#094771" : "transparent",
                color: i === active ? "#ffffff" : "#cccccc",
                transition: "background 0.1s",
              }}
            >
              {/* Wrench icon like VS Code quick fix */}
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0, opacity: 0.7 }}>
                <path d="M10.5 1.5a4 4 0 0 1 .5 7.96V15h-1v-5.54A4 4 0 0 1 10.5 1.5zm0 1a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM5 1v4H3V1H2v4.5A1.5 1.5 0 0 0 3.5 7H4v8h1V7h.5A1.5 1.5 0 0 0 7 5.5V1H5z"
                  fill="currentColor"/>
              </svg>
              <span style={{ fontWeight: i === active ? 600 : 400 }}>{r.value}</span>
              {i === active && (
                <span style={{ marginLeft: "auto", fontSize: 11, opacity: 0.6 }}>↵</span>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <div style={{ padding: "10px 12px", color: "#888", fontStyle: "italic" }}>
          No suggestions available
        </div>
      )}

      {/* Footer hint */}
      <div style={{
        padding: "5px 12px",
        borderTop: "1px solid #333",
        display: "flex",
        gap: 12,
        color: "#666",
        fontSize: 11,
      }}>
        <span><kbd style={kbd}>↑↓</kbd> navigate</span>
        <span><kbd style={kbd}>↵</kbd> apply</span>
        <span><kbd style={kbd}>Esc</kbd> dismiss</span>
      </div>
    </div>
  );
}

const kbd: React.CSSProperties = {
  background: "#2d2d2d",
  border: "1px solid #555",
  borderRadius: 3,
  padding: "1px 4px",
  fontFamily: "monospace",
  fontSize: 10,
  color: "#aaa",
};
