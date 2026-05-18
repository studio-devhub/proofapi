// Example: floating suggestion popup shown when user clicks an underlined word.

import { useEffect, useRef } from "react";
import type { Match } from "../types/proof";

interface Props {
  match: Match;
  position: { x: number; y: number };
  onApply: (value: string) => void;
  onDismiss: () => void;
}

const ICONS: Record<string, string> = {
  misspelling:   "🔴",
  grammar:       "🟡",
  style:         "🔵",
  typographical: "🟠",
};

export default function SuggestionPopup({ match, position, onApply, onDismiss }: Props) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onDismiss();
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [onDismiss]);

  const left = Math.min(position.x, window.innerWidth  - 288 - 8);
  const top  = Math.min(position.y + 8, window.innerHeight - 200 - 8);

  return (
    <div
      ref={ref}
      style={{
        position: "fixed", zIndex: 50, width: 288,
        background: "#fff", borderRadius: 12,
        boxShadow: "0 4px 24px rgba(0,0,0,0.12)",
        border: "1px solid #f0f0f0", overflow: "hidden",
        top, left,
      }}
    >
      <div style={{ padding: "12px 16px 10px", borderBottom: "1px solid #f5f5f5" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
          <span>{ICONS[match.rule.issueType] ?? "⚪"}</span>
          <span style={{ fontSize: 11, fontWeight: 600, color: "#999", textTransform: "uppercase", letterSpacing: 1 }}>
            {match.rule.category.name}
          </span>
        </div>
        <p style={{ margin: 0, fontSize: 14, color: "#333" }}>{match.message}</p>
      </div>

      {match.replacements.length > 0 && (
        <div style={{ padding: "10px 16px" }}>
          <p style={{ margin: "0 0 8px", fontSize: 12, color: "#999" }}>Suggestions</p>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {match.replacements.slice(0, 5).map((r) => (
              <button
                key={r.value}
                onClick={() => onApply(r.value)}
                style={{
                  padding: "4px 12px", fontSize: 13, fontWeight: 500,
                  background: "#eff6ff", color: "#2563eb",
                  border: "none", borderRadius: 999, cursor: "pointer",
                }}
              >
                {r.value}
              </button>
            ))}
          </div>
        </div>
      )}

      <div style={{ padding: "8px 16px", borderTop: "1px solid #f5f5f5", textAlign: "right" }}>
        <button
          onClick={onDismiss}
          style={{ fontSize: 12, color: "#aaa", background: "none", border: "none", cursor: "pointer" }}
        >
          Dismiss
        </button>
      </div>
    </div>
  );
}
