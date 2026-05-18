# ProofAPI — Frontend Integration Guide

This guide covers everything needed to integrate ProofAPI into a **React (Vite)** application.

---

## Setup

### 1. Environment Variables

Create a `.env` file in your React project root:

```env
VITE_PROOF_API_URL=http://localhost:4003
VITE_PROOF_API_KEY=your-api-key
VITE_PROOF_WS_URL=ws://localhost:4003/v1/ws
```

> **Production:** Replace with your deployed API URL and use `wss://` for WebSocket.

---

## Option 1 — REST API

Best for: submit-on-button-click, form validation, document checking.

### TypeScript Types

```typescript
// src/types/proof.ts

export interface CheckRequest {
  text: string;
  language?: string; // default: "en-US"
  level?: string;    // "default" | "picky"
}

export interface Match {
  message: string;
  offset: number;
  length: number;
  replacements: { value: string }[];
  rule: {
    id: string;
    description: string;
    issueType: string;
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
```

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

### React Hook

```typescript
// src/hooks/useProofCheck.ts
import { useState, useCallback } from 'react';
import { checkText } from '@/lib/proofApi';
import type { Match } from '@/types/proof';

export function useProofCheck() {
  const [matches, setMatches]   = useState<Match[]>([]);
  const [loading, setLoading]   = useState(false);
  const [error, setError]       = useState<string | null>(null);
  const [cached, setCached]     = useState(false);

  const check = useCallback(async (text: string, language = 'en-US') => {
    if (text.trim().length < 2) return;

    setLoading(true);
    setError(null);

    try {
      const result = await checkText({ text, language });
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

  const handleCheck = () => check(text);

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    reset();
  };

  return (
    <div>
      <textarea
        value={text}
        onChange={handleChange}
        placeholder="Enter text to check..."
        rows={6}
        style={{ width: '100%' }}
      />

      <button onClick={handleCheck} disabled={loading || text.length < 2}>
        {loading ? 'Checking...' : 'Check Grammar'}
      </button>

      {cached && <small> (from cache)</small>}

      {error && <p style={{ color: 'red' }}>{error}</p>}

      {!loading && matches.length === 0 && text.length > 0 && (
        <p style={{ color: 'green' }}>No issues found!</p>
      )}

      {matches.map((match, i) => (
        <div key={i} style={{ borderLeft: '3px solid red', padding: '8px 12px', marginTop: 8 }}>
          <strong>{match.rule.category.name}</strong>
          <p>{match.message}</p>
          <p>
            Error: <code>{text.slice(match.offset, match.offset + match.length)}</code>
          </p>
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

## Option 2 — WebSocket API

Best for: real-time checking as the user types (editor, text input, rich text).

The server automatically debounces 150ms — no need to debounce on the frontend.

### WebSocket Client Class

```typescript
// src/lib/ProofWSClient.ts
import type { Match } from '@/types/proof';

type ResultHandler = (matches: Match[], latencyMs: number, cached: boolean) => void;
type StatusHandler = (connected: boolean) => void;

export class ProofWSClient {
  private ws: WebSocket | null = null;
  private seq = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private destroyed = false;

  constructor(
    private readonly url: string,
    private readonly apiKey: string,
    private readonly handlers: {
      onResult: ResultHandler;
      onStatus?: StatusHandler;
      onError?: (msg: string) => void;
    }
  ) {}

  connect() {
    if (this.destroyed) return;

    this.ws = new WebSocket(`${this.url}?api_key=${this.apiKey}`);

    this.ws.onopen = () => {
      this.handlers.onStatus?.(true);
      this.startPing();
    };

    this.ws.onmessage = (event) => {
      const msg = JSON.parse(event.data as string);
      if (msg.type === 'result') {
        this.handlers.onResult(
          msg.payload.matches,
          msg.payload.latencyMs,
          msg.payload.cached
        );
      } else if (msg.type === 'error') {
        this.handlers.onError?.(msg.error);
      }
    };

    this.ws.onclose = () => {
      this.handlers.onStatus?.(false);
      this.stopPing();
      if (!this.destroyed) {
        this.reconnectTimer = setTimeout(() => this.connect(), 3000);
      }
    };

    this.ws.onerror = () => this.ws?.close();
  }

  check(text: string, language = 'en-US') {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({
      type: 'check',
      text,
      language,
      seqId: ++this.seq,
    }));
  }

  destroy() {
    this.destroyed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.stopPing();
    this.ws?.close();
  }

  private startPing() {
    this.pingTimer = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30_000);
  }

  private stopPing() {
    if (this.pingTimer) clearInterval(this.pingTimer);
  }
}
```

### React Hook for WebSocket

```typescript
// src/hooks/useProofWS.ts
import { useEffect, useRef, useState, useCallback } from 'react';
import { ProofWSClient } from '@/lib/ProofWSClient';
import type { Match } from '@/types/proof';

export function useProofWS() {
  const clientRef               = useRef<ProofWSClient | null>(null);
  const [matches, setMatches]   = useState<Match[]>([]);
  const [latencyMs, setLatency] = useState<number | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const client = new ProofWSClient(
      import.meta.env.VITE_PROOF_WS_URL,
      import.meta.env.VITE_PROOF_API_KEY,
      {
        onResult: (m, latency) => {
          setMatches(m);
          setLatency(latency);
        },
        onStatus: setConnected,
        onError: (err) => console.error('[ProofAPI]', err),
      }
    );

    clientRef.current = client;
    client.connect();

    return () => client.destroy();
  }, []);

  const check = useCallback((text: string, language?: string) => {
    clientRef.current?.check(text, language);
  }, []);

  return { matches, latencyMs, connected, check };
}
```

### Live Editor Component

```tsx
// src/components/LiveEditor.tsx
import { useState, useCallback } from 'react';
import { useProofWS } from '@/hooks/useProofWS';

export function LiveEditor() {
  const { matches, latencyMs, connected, check } = useProofWS();
  const [text, setText] = useState('');

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = e.target.value;
    setText(value);
    if (value.trim().length >= 2) check(value);
  }, [check]);

  return (
    <div style={{ fontFamily: 'sans-serif', maxWidth: 680 }}>

      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
        <span style={{ fontSize: 13, color: connected ? '#38a169' : '#e53e3e' }}>
          {connected ? '● Connected' : '○ Connecting...'}
        </span>
        {latencyMs !== null && (
          <span style={{ fontSize: 12, color: '#718096' }}>{latencyMs}ms</span>
        )}
      </div>

      <textarea
        value={text}
        onChange={handleChange}
        placeholder="Start typing — grammar is checked in real time..."
        rows={8}
        style={{ width: '100%', fontSize: 15, padding: 12, boxSizing: 'border-box' }}
      />

      <p style={{ color: matches.length > 0 ? '#e53e3e' : '#38a169', margin: '4px 0' }}>
        {text.length < 2 ? '' : matches.length > 0 ? `${matches.length} issue(s) found` : 'All good!'}
      </p>

      {matches.map((match, i) => (
        <div key={i} style={{
          marginTop: 8, padding: '10px 14px',
          borderLeft: '3px solid #e53e3e',
          background: '#fff5f5', borderRadius: 4,
        }}>
          <strong style={{ fontSize: 13, color: '#c53030' }}>
            {match.rule.category.name}
          </strong>
          <p style={{ margin: '4px 0', fontSize: 14 }}>{match.message}</p>
          {match.replacements.length > 0 && (
            <p style={{ margin: 0, fontSize: 13 }}>
              Suggestions:{' '}
              {match.replacements.slice(0, 4).map((r, j) => (
                <code key={j} style={{
                  marginRight: 6, padding: '2px 7px',
                  background: '#ebf8ff', borderRadius: 3,
                }}>
                  {r.value}
                </code>
              ))}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}
```

---

## Highlight Errors in Text

Use `offset` + `length` to visually mark errors inside rendered text:

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
    segments.push({
      text: text.slice(match.offset, match.offset + match.length),
      match,
    });
    cursor = match.offset + match.length;
  }

  if (cursor < text.length) {
    segments.push({ text: text.slice(cursor) });
  }

  return segments;
}
```

```tsx
// src/components/HighlightedText.tsx
import { getSegments } from '@/lib/highlight';
import type { Match } from '@/types/proof';

export function HighlightedText({ text, matches }: { text: string; matches: Match[] }) {
  return (
    <p style={{ lineHeight: 1.7, fontSize: 15 }}>
      {getSegments(text, matches).map((seg, i) =>
        seg.match ? (
          <mark
            key={i}
            title={seg.match.message}
            style={{
              background: 'transparent',
              borderBottom: '2px solid #e53e3e',
              cursor: 'help',
            }}
          >
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

## Suggested File Structure

```text
src/
├── types/
│   └── proof.ts           # TypeScript interfaces
├── lib/
│   ├── proofApi.ts        # REST client
│   ├── ProofWSClient.ts   # WebSocket client class
│   └── highlight.ts       # Text highlight utility
├── hooks/
│   ├── useProofCheck.ts   # REST hook
│   └── useProofWS.ts      # WebSocket hook
└── components/
    ├── GrammarChecker.tsx  # Submit-on-click checker
    ├── LiveEditor.tsx      # Real-time WebSocket editor
    └── HighlightedText.tsx # Inline error highlighting
```

> **Working example with TipTap rich text editor:** `examples/react/` in the repo root.

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
| Check as user types | WebSocket via `useProofWS` |
| Highlight errors inline | `getSegments()` + `HighlightedText` |
| List available languages | REST `GET /v1/languages` |
| Monitor API health | REST `GET /v1/health` |
