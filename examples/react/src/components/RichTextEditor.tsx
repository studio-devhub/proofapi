import { useEditor, EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { useEffect, useState, useCallback, useRef } from "react";

import { useSpellCheck } from "../hooks/useSpellCheck";
import { SpellCheckExtension, applyMatches } from "../extensions/SpellCheckExtension";
import SuggestionPopup from "./SuggestionPopup";
import type { Match } from "../types/proof";

// ── Status dot ────────────────────────────────────────────
const DOT: Record<string, string> = {
  connected:    "bg-green-400",
  connecting:   "bg-yellow-400 animate-pulse",
  disconnected: "bg-red-400 animate-pulse",
  error:        "bg-red-600",
};

// ── Error type badge colors ───────────────────────────────
const BADGE: Record<string, string> = {
  misspelling:   "bg-red-100    text-red-600",
  grammar:       "bg-yellow-100 text-yellow-700",
  style:         "bg-blue-100   text-blue-700",
  typographical: "bg-orange-100 text-orange-700",
};

export default function RichTextEditor() {
  const { check, result, status, clearMatches } = useSpellCheck();

  const [popup, setPopup] = useState<{
    match: Match;
    x: number;
    y: number;
  } | null>(null);

  const matchesRef = useRef<Match[]>([]);

  // ── Editor setup ──────────────────────────────────────
  const editor = useEditor({
    extensions: [
      StarterKit,
      SpellCheckExtension.configure({
        onTextChange: (text) => {
          if (!text.trim()) { clearMatches(); return; }
          check(text);
        },
      }),
    ],
    content: "<p>Start typing to see real-time spell check in action...</p>",
    editorProps: {
      attributes: { class: "outline-none min-h-[160px] prose prose-sm max-w-none" },
    },
  });

  // ── Push WS results → editor decorations ─────────────
  useEffect(() => {
    if (editor && result) {
      applyMatches(editor, result.matches);
      matchesRef.current = result.matches;
    }
  }, [editor, result]);

  // ── Cmd+. → show suggestion for word under cursor ─────
  useEffect(() => {
    if (!editor) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key !== ".") return;
      e.preventDefault();

      const { from } = editor.state.selection;
      // +1 offset because ProseMirror positions start at 1, LT offsets at 0
      const pos = from - 1;
      const match = matchesRef.current.find(
        (m) => pos >= m.offset && pos < m.offset + m.length
      ) ?? matchesRef.current.find(
        // fallback: cursor is right after the word (end boundary)
        (m) => pos === m.offset + m.length
      );
      if (!match) return;

      // Position popup near the cursor caret
      // Anchor popup to start of the matched word, not cursor position
      const wordStart = editor.view.coordsAtPos(match.offset + 1);
      setPopup({ match, x: wordStart.left, y: wordStart.bottom });
    };

    const dom = editor.view.dom;
    dom.addEventListener("keydown", handleKeyDown);
    return () => dom.removeEventListener("keydown", handleKeyDown);
  }, [editor]);

  // ── Suggestion popup ──────────────────────────────────
  const handleClick = useCallback((e: React.MouseEvent) => {
    const el = e.target as HTMLElement;
    const raw = el.getAttribute("data-match");
    if (!raw) { setPopup(null); return; }
    const match: Match = JSON.parse(raw);
    const rect = el.getBoundingClientRect();
    setPopup({ match, x: rect.left, y: rect.bottom });
  }, []);

  const applySuggestion = useCallback((replacement: string) => {
    if (!editor || !popup) return;
    const { offset, length } = popup.match;
    // ProseMirror positions are 1-based; LT offsets are 0-based
    const from = offset + 1;
    const to   = offset + length + 1;
    editor
      .chain()
      .focus()
      .deleteRange({ from, to })
      .insertContentAt(from, replacement)
      .run();
    setPopup(null);
  }, [editor, popup]);

  // ── Stats ─────────────────────────────────────────────
  const matches   = result?.matches ?? [];
  const byType    = matches.reduce((acc, m) => {
    acc[m.rule.issueType] = (acc[m.rule.issueType] ?? 0) + 1;
    return acc;
  }, {} as Record<string, number>);

  return (
    <div className="max-w-3xl mx-auto p-6 space-y-3">

      {/* Top bar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className={`w-2 h-2 rounded-full ${DOT[status]}`} />
          <span className="text-xs text-gray-500 capitalize">{status}</span>
          {result?.cached && (
            <span className="text-xs text-gray-400">⚡ cached</span>
          )}
          {result && !result.cached && result.latencyMs > 0 && (
            <span className="text-xs text-gray-400">{result.latencyMs}ms</span>
          )}
        </div>

        {/* Issue badges */}
        <div className="flex items-center gap-1.5">
          {matches.length === 0 && status === "connected" && (
            <span className="text-xs text-green-500 font-medium">✓ No issues</span>
          )}
          {Object.entries(byType).map(([type, count]) => (
            <span
              key={type}
              className={`text-xs px-2 py-0.5 rounded-full font-medium ${BADGE[type] ?? "bg-gray-100 text-gray-600"}`}
            >
              {count} {type === "misspelling" ? "spelling" : type}
            </span>
          ))}
        </div>
      </div>

      {/* Editor */}
      <div
        className="bg-white border border-gray-200 rounded-xl px-5 py-4 shadow-sm cursor-text"
        onClick={handleClick}
      >
        <EditorContent editor={editor} />
      </div>

      {/* Legend */}
      <div className="flex items-center justify-between">
      <div className="flex gap-4">
        {[
          { cls: "sp-spell",   label: "Spelling"    },
          { cls: "sp-grammar", label: "Grammar"     },
          { cls: "sp-style",   label: "Style"       },
          { cls: "sp-punct",   label: "Punctuation" },
        ].map(({ cls, label }) => (
          <div key={label} className="flex items-center gap-1.5">
            <div className={`h-0.5 w-4 ${cls}-sample`} />
            <span className="text-xs text-gray-400">{label}</span>
          </div>
        ))}
      </div>
        <span className="text-xs text-gray-400">
          Click or <kbd className="px-1 py-0.5 rounded bg-gray-100 text-gray-500 font-mono text-xs">⌘.</kbd> for suggestions
        </span>
      </div>

      {/* Suggestion popup */}
      {popup && (
        <SuggestionPopup
          match={popup.match}
          position={{ x: popup.x, y: popup.y }}
          onApply={applySuggestion}
          onDismiss={() => setPopup(null)}
        />
      )}

      <style>{`
        .sp-spell   { text-decoration: underline wavy #ef4444; }
        .sp-grammar { text-decoration: underline wavy #eab308; }
        .sp-style   { text-decoration: underline wavy #3b82f6; }
        .sp-punct   { text-decoration: underline wavy #f97316; }

        .sp-spell-sample   { background: #ef4444; }
        .sp-grammar-sample { background: #eab308; }
        .sp-style-sample   { background: #3b82f6; }
        .sp-punct-sample   { background: #f97316; }
      `}</style>
    </div>
  );
}
