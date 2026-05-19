# ProofAPI — Frontend Integration Guide

Complete integration guide for **React (Vite + TypeScript)** applications.

> **Working example with TipTap rich text editor, VS Code-style suggestions, and real-time wavy underlines:** [`examples/react/`](examples/react/)

---

## Setup

```bash
cd examples/react
cp .env.example .env   # add your API key
yarn install
yarn dev               # http://localhost:5173
```

### Environment Variables

```env
# Local development
VITE_PROOF_API_URL=http://localhost:4003
VITE_PROOF_API_KEY=your-api-key
VITE_PROOF_WS_URL=ws://localhost:4003/v1/ws

# Production
VITE_PROOF_API_URL=https://proofapi.tulvo.io
VITE_PROOF_API_KEY=your-api-key
VITE_PROOF_WS_URL=wss://proofapi.tulvo.io/v1/ws
```

---

## TypeScript Types

```typescript
// src/types/proof.ts

export interface CheckRequest {
  text: string;
  language?: string;           // default: "en-US"
  level?: string;              // "default" | "picky" — default: "picky"
  motherTongue?: string;       // native language for false-friends detection e.g. "de-DE"
  enabledCategories?: string;  // default: all 10 categories
  disabledCategories?: string;
  enabledRules?: string;
  disabledRules?: string;
  enabledOnly?: boolean;
}

export interface Match {
  message: string;
  offset: number;
  length: number;
  replacements: { value: string }[];
  rule: {
    id: string;
    description: string;
    issueType: "misspelling" | "grammar" | "style" | "typographical" | string;
    category: { id: string; name: string };
  };
  context: { text: string; offset: number; length: number };
}

export interface CheckResponse {
  matches: Match[];
  language: { name: string; code: string };
  checkedAt: string;
  cached: boolean;
  cacheExpiresIn?: number;
}

export interface SpellCheckResult {
  matches: Match[];
  cached: boolean;
  latencyMs: number;
}
```

---

## Option 1 — REST API

Best for: submit-on-click, form validation, document checking.

### API Client

```typescript
// src/lib/proofApi.ts
import type { CheckRequest, CheckResponse } from '@/types/proof';

const BASE_URL = import.meta.env.VITE_PROOF_API_URL;
const API_KEY  = import.meta.env.VITE_PROOF_API_KEY;

export async function checkText(req: CheckRequest): Promise<CheckResponse> {
  const res = await fetch(`${BASE_URL}/v1/check`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': API_KEY,
    },
    body: JSON.stringify(req),
  });

  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error ?? 'Request failed');
  }

  return res.json();
}

export async function getLanguages(): Promise<{ code: string; longCode: string; name: string }[]> {
  const res = await fetch(`${BASE_URL}/v1/languages`, {
    headers: { 'X-API-Key': API_KEY },
  });
  return res.json();
}
```

### Maximum suggestions request

```typescript
await checkText({
  text: 'Your text here...',
  language: 'en-US',
  level: 'picky',
  enabledCategories: 'GRAMMAR,SPELLING,STYLE,PUNCTUATION,TYPOGRAPHY,CASING,CONFUSED_WORDS,REDUNDANCY,COMPOUNDING,MISC',
});
```

### React Hook

```typescript
// src/hooks/useProofCheck.ts
import { useState, useCallback } from 'react';
import { checkText } from '@/lib/proofApi';
import type { Match } from '@/types/proof';

export function useProofCheck() {
  const [matches, setMatches] = useState<Match[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError]     = useState<string | null>(null);
  const [cached, setCached]   = useState(false);

  const check = useCallback(async (text: string, language = 'en-US') => {
    if (text.trim().length < 2) return;
    setLoading(true);
    setError(null);
    try {
      const result = await checkText({
        text,
        language,
        level: 'picky',
        enabledCategories: 'GRAMMAR,SPELLING,STYLE,PUNCTUATION,TYPOGRAPHY,CASING,CONFUSED_WORDS,REDUNDANCY',
      });
      setMatches(result.matches);
      setCached(result.cached);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Check failed');
    } finally {
      setLoading(false);
    }
  }, []);

  const reset = useCallback(() => {
    setMatches([]);
    setError(null);
    setCached(false);
  }, []);

  return { matches, loading, error, cached, check, reset };
}
```

### Component Example

```tsx
// src/components/GrammarChecker.tsx
import { useState } from 'react';
import { useProofCheck } from '@/hooks/useProofCheck';

export function GrammarChecker() {
  const [text, setText] = useState('');
  const { matches, loading, error, cached, check, reset } = useProofCheck();

  return (
    <div>
      <textarea
        value={text}
        onChange={(e) => { setText(e.target.value); reset(); }}
        placeholder="Enter text to check..."
        rows={6}
        style={{ width: '100%' }}
      />
      <button onClick={() => check(text)} disabled={loading || text.length < 2}>
        {loading ? 'Checking...' : 'Check Grammar'}
      </button>

      {cached && <small> (from cache)</small>}
      {error && <p style={{ color: 'red' }}>{error}</p>}

      {matches.map((match, i) => (
        <div key={i} style={{ borderLeft: '3px solid red', padding: '8px 12px', marginTop: 8 }}>
          <strong>{match.rule.category.name}</strong>
          <p>{match.message}</p>
          <p>Suggestions: {match.replacements.slice(0, 4).map(r => r.value).join(' · ')}</p>
        </div>
      ))}
    </div>
  );
}
```

---

## Option 2 — WebSocket API

Best for: real-time checking as the user types. The server debounces 150ms — no client-side debounce needed.

> **WebSocket default:** if `level`/`enabledCategories` are omitted, the server automatically uses `level=picky` with all major categories for maximum accuracy.

### WebSocket Hook

```typescript
// src/hooks/useSpellCheck.ts
import { useEffect, useRef, useCallback, useState } from 'react';
import type { SpellCheckResult } from '@/types/proof';

type Status = 'connecting' | 'connected' | 'disconnected' | 'error';

const WS_URL  = import.meta.env.VITE_PROOF_WS_URL  ?? 'ws://localhost:4003/v1/ws';
const API_KEY = import.meta.env.VITE_PROOF_API_KEY ?? '';

export function useSpellCheck() {
  const wsRef        = useRef<WebSocket | null>(null);
  const seqRef       = useRef(0);
  const reconnectRef = useRef<ReturnType<typeof setTimeout>>();

  const [result, setResult]   = useState<SpellCheckResult | null>(null);
  const [status, setStatus]   = useState<Status>('connecting');

  const connect = useCallback(() => {
    wsRef.current?.close();
    const ws = new WebSocket(`${WS_URL}?api_key=${API_KEY}`);
    wsRef.current = ws;

    ws.onopen    = () => { setStatus('connected'); clearTimeout(reconnectRef.current); };
    ws.onclose   = () => { setStatus('disconnected'); reconnectRef.current = setTimeout(connect, 2000); };
    ws.onerror   = () => { setStatus('error'); ws.close(); };
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      if (msg.type === 'result' && msg.payload) {
        setResult({
          matches:   msg.payload.matches   ?? [],
          cached:    msg.payload.cached    ?? false,
          latencyMs: msg.payload.latencyMs ?? 0,
        });
      }
    };
  }, []);

  useEffect(() => {
    connect();
    return () => { clearTimeout(reconnectRef.current); wsRef.current?.close(); };
  }, [connect]);

  const check = useCallback((text: string, language = 'en-US') => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({
      type: 'check',
      text,
      language,
      // omit level/enabledCategories to use server defaults (picky + all categories)
      seqId: ++seqRef.current,
    }));
  }, []);

  const clearMatches = useCallback(() => setResult(null), []);

  return { check, result, status, clearMatches };
}
```

### Live Editor Component

```tsx
// src/components/LiveEditor.tsx
import { useState, useCallback } from 'react';
import { useSpellCheck } from '@/hooks/useSpellCheck';

export function LiveEditor() {
  const { result, status, check } = useSpellCheck();
  const [text, setText] = useState('');

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = e.target.value;
    setText(value);
    if (value.trim().length >= 2) check(value);
  }, [check]);

  const matches = result?.matches ?? [];

  return (
    <div>
      <span style={{ color: status === 'connected' ? 'green' : 'red' }}>
        {status === 'connected' ? '● Connected' : '○ Connecting...'}
      </span>
      {result && <span> {result.latencyMs}ms {result.cached ? '⚡ cached' : ''}</span>}

      <textarea value={text} onChange={handleChange} rows={8} style={{ width: '100%' }} />

      <p>{matches.length > 0 ? `${matches.length} issue(s) found` : text.length >= 2 ? 'All good!' : ''}</p>

      {matches.map((match, i) => (
        <div key={i} style={{ borderLeft: '3px solid red', padding: '8px 12px', marginTop: 8 }}>
          <strong>{match.rule.category.name}</strong>
          <p>{match.message}</p>
          {match.replacements.length > 0 && (
            <p>Suggestions: {match.replacements.slice(0, 4).map(r => r.value).join(' · ')}</p>
          )}
        </div>
      ))}
    </div>
  );
}
```

---

## Option 3 — TipTap Rich Text Editor

The [`examples/react/`](examples/react/) example shows full TipTap integration with:

- **Wavy underlines** — colour-coded by issue type (red=spelling, yellow=grammar, blue=style, orange=punctuation)
- **VS Code-style suggestion popup** — dark theme, anchored to the word, keyboard navigable
- **Cmd+.** (or **Ctrl+.**) shortcut to open suggestions at cursor position
- **Click** on underlined word to open popup
- **↑↓** navigate suggestions, **Enter** apply, **Esc** dismiss
- **Auto-reconnect** WebSocket with exponential backoff

```bash
cd examples/react
cp .env.example .env
yarn install && yarn dev
```

---

## Highlight Errors in Plain Text

Use `offset` + `length` from each match to render inline highlights:

```typescript
// src/lib/highlight.ts
import type { Match } from '@/types/proof';

export function getSegments(text: string, matches: Match[]) {
  const sorted = [...matches].sort((a, b) => a.offset - b.offset);
  const segments: { text: string; match?: Match }[] = [];
  let cursor = 0;

  for (const match of sorted) {
    if (match.offset > cursor) {
      segments.push({ text: text.slice(cursor, match.offset) });
    }
    segments.push({ text: text.slice(match.offset, match.offset + match.length), match });
    cursor = match.offset + match.length;
  }

  if (cursor < text.length) segments.push({ text: text.slice(cursor) });
  return segments;
}
```

```tsx
// src/components/HighlightedText.tsx
import { getSegments } from '@/lib/highlight';
import type { Match } from '@/types/proof';

const UNDERLINE: Record<string, string> = {
  misspelling:   'underline wavy #ef4444',
  grammar:       'underline wavy #eab308',
  style:         'underline wavy #3b82f6',
  typographical: 'underline wavy #f97316',
};

export function HighlightedText({ text, matches }: { text: string; matches: Match[] }) {
  return (
    <p style={{ lineHeight: 1.7 }}>
      {getSegments(text, matches).map((seg, i) =>
        seg.match ? (
          <mark key={i} title={seg.match.message}
            style={{ background: 'transparent', textDecoration: UNDERLINE[seg.match.rule.issueType] ?? 'underline wavy #888' }}>
            {seg.text}
          </mark>
        ) : (
          <span key={i}>{seg.text}</span>
        )
      )}
    </p>
  );
}
```

---

## Supported Languages

Pass as the `language` field in any request:

| Code | Language |
| ---- | -------- |
| `en-US` | English (US) |
| `en-GB` | English (UK) |
| `de-DE` | German |
| `fr` | French |
| `es` | Spanish |
| `pt-BR` | Portuguese (Brazil) |
| `ar` | Arabic |
| `zh-CN` | Chinese |
| `ru-RU` | Russian |
| `ja-JP` | Japanese |

Full list: `GET /v1/languages`

---

## Error Handling Reference

| Status | Meaning | Action |
| ------ | ------- | ------ |
| `200` | Success | Render matches |
| `400` | Invalid input | Show validation message |
| `401` | Wrong API key | Check `VITE_PROOF_API_KEY` |
| `429` | Rate limited | Reduce request frequency |
| `503` | Service down | Show retry message |

---

## Quick Reference

| Use Case | Approach |
| -------- | -------- |
| Check on button click | REST `POST /v1/check` via `useProofCheck` |
| Real-time as user types | WebSocket via `useSpellCheck` |
| Maximum suggestions | `level=picky` + `enabledCategories=GRAMMAR,SPELLING,STYLE,...` |
| Rich text editor | TipTap example in `examples/react/` |
| Highlight errors inline | `getSegments()` + `HighlightedText` |
| List available languages | `GET /v1/languages` |
| Monitor API health | `GET /v1/health` |
| Explore API interactively | Swagger UI at `/docs/index.html` |
