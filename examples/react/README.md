# ProofAPI — React Integration Example

> **This is example code only.** Copy what you need into your own project — do not use this folder as-is.

A working React (Vite + TypeScript) example showing how to integrate ProofAPI for real-time grammar and spell checking using a TipTap rich text editor.

---

## What's included

```
src/
├── types/
│   └── proof.ts                  # Shared TypeScript interfaces (Match, SpellCheckResult)
├── hooks/
│   └── useSpellCheck.ts          # WebSocket hook — connects, reconnects, sends checks
├── extensions/
│   └── SpellCheckExtension.ts    # TipTap ProseMirror plugin — renders wavy underlines
├── components/
│   ├── RichTextEditor.tsx         # Main editor — wires hook + extension + popup together
│   └── SuggestionPopup.tsx        # Floating popup shown on clicking an underlined word
└── App.tsx                        # Root component
```

---

## Setup

```bash
npm install
cp .env.example .env        # add your API key
npm run dev
```

### Dependencies

```bash
npm install @tiptap/react @tiptap/pm @tiptap/starter-kit
```

### Environment

```env
VITE_PROOF_WS_URL=ws://localhost:4003/v1/ws
VITE_PROOF_API_KEY=your-api-key-here
```

---

## How it works

1. `useSpellCheck` opens a WebSocket connection to ProofAPI on mount
2. On every editor change, `SpellCheckExtension` fires `onTextChange` with the plain text
3. `useSpellCheck` sends a `check` message — server debounces 150ms automatically
4. When results arrive, `applyMatches()` pushes decorations into the ProseMirror editor
5. Clicking an underlined word opens `SuggestionPopup` with correction options
6. Selecting a suggestion replaces the erroneous text in the editor

---

## Adapting to your project

| If you want | Use |
| ----------- | --- |
| WebSocket only (no TipTap) | `useSpellCheck.ts` |
| Plain textarea instead of rich editor | `useSpellCheck.ts` + custom highlight |
| REST API instead of WebSocket | See `FRONTEND_INTEGRATION.md` in the repo root |
