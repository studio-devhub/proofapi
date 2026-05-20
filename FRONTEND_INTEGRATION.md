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
  clientId?: string;           // optional: filter matches against this client's dictionary
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

// ── Dictionary ────────────────────────────────────────────

export interface DictionaryWord {
  word: string;
  language?: string;
  addedAt: string; // ISO 8601
}

export interface DictionaryListResponse {
  clientId: string;
  words: DictionaryWord[];
  count: number;
}
```

---

## Option 1 — REST API

Best for: submit-on-click, form validation, document checking.

### API Client

```typescript
// src/lib/proofApi.ts
import type { CheckRequest, CheckResponse, DictionaryWord, DictionaryListResponse } from '@/types/proof';

const BASE_URL = import.meta.env.VITE_PROOF_API_URL;
const API_KEY  = import.meta.env.VITE_PROOF_API_KEY;

// ── Grammar Check ─────────────────────────────────────────

export async function checkText(req: CheckRequest, clientId?: string): Promise<CheckResponse> {
  const res = await fetch(`${BASE_URL}/v1/check`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': API_KEY,
      ...(clientId ? { 'X-Client-ID': clientId } : {}),
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

// ── Dictionary ────────────────────────────────────────────

function dictHeaders(clientId: string) {
  return {
    'Content-Type': 'application/json',
    'X-API-Key': API_KEY,
    'X-Client-ID': clientId,
  };
}

/** Add a word to the client's dictionary. */
export async function addDictionaryWord(
  clientId: string,
  word: string,
  language = 'en-US',
): Promise<DictionaryWord> {
  const res = await fetch(`${BASE_URL}/v1/dictionary/words`, {
    method: 'POST',
    headers: dictHeaders(clientId),
    body: JSON.stringify({ word, language }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error ?? 'Failed to add word');
  }
  return res.json();
}

/** Remove a word from the client's dictionary. */
export async function removeDictionaryWord(clientId: string, word: string): Promise<void> {
  await fetch(`${BASE_URL}/v1/dictionary/words/${encodeURIComponent(word)}`, {
    method: 'DELETE',
    headers: dictHeaders(clientId),
  });
}

/** List all words in the client's dictionary. */
export async function listDictionaryWords(clientId: string): Promise<DictionaryListResponse> {
  const res = await fetch(`${BASE_URL}/v1/dictionary/words`, {
    headers: dictHeaders(clientId),
  });
  return res.json();
}

/** Clear all words from the client's dictionary. */
export async function clearDictionary(clientId: string): Promise<void> {
  await fetch(`${BASE_URL}/v1/dictionary`, {
    method: 'DELETE',
    headers: dictHeaders(clientId),
  });
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

export function useProofCheck(clientId?: string) {
  const [matches, setMatches] = useState<Match[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError]     = useState<string | null>(null);
  const [cached, setCached]   = useState(false);

  const check = useCallback(async (text: string, language = 'en-US') => {
    if (text.trim().length < 2) return;
    setLoading(true);
    setError(null);
    try {
      const result = await checkText(
        { text, language, level: 'picky',
          enabledCategories: 'GRAMMAR,SPELLING,STYLE,PUNCTUATION,TYPOGRAPHY,CASING,CONFUSED_WORDS,REDUNDANCY' },
        clientId,
      );
      setMatches(result.matches);
      setCached(result.cached);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Check failed');
    } finally {
      setLoading(false);
    }
  }, [clientId]);

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

## Custom Dictionary

Users can add words (brand names, domain terms, proper nouns) to their personal dictionary. Future spell checks with that `clientId` will silently skip matches for those words.

> **How it works:** The LT result cache is shared across all users. Dictionary filtering is applied **after** cache lookup — per request, per clientId. Adding a word never invalidates the shared cache.

### clientId

`clientId` is any stable identifier for the user in your system (user ID, UUID, email hash, etc.). It must be 1–128 alphanumeric characters (`a-z A-Z 0-9 - _ .`).

```typescript
const clientId = currentUser.id; // e.g. "usr_abc123" or a UUID
```

### Dictionary Hook

```typescript
// src/hooks/useDictionary.ts
import { useState, useEffect, useCallback } from 'react';
import {
  listDictionaryWords,
  addDictionaryWord,
  removeDictionaryWord,
  clearDictionary,
} from '@/lib/proofApi';
import type { DictionaryWord } from '@/types/proof';

export function useDictionary(clientId: string) {
  const [words, setWords]     = useState<DictionaryWord[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError]     = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!clientId) return;
    try {
      const res = await listDictionaryWords(clientId);
      setWords(res.words);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load dictionary');
    }
  }, [clientId]);

  useEffect(() => { load(); }, [load]);

  const addWord = useCallback(async (word: string, language = 'en-US') => {
    setLoading(true);
    setError(null);
    try {
      await addDictionaryWord(clientId, word, language);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add word');
    } finally {
      setLoading(false);
    }
  }, [clientId, load]);

  const removeWord = useCallback(async (word: string) => {
    setLoading(true);
    try {
      await removeDictionaryWord(clientId, word);
      setWords(prev => prev.filter(w => w.word !== word));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove word');
    } finally {
      setLoading(false);
    }
  }, [clientId]);

  const clear = useCallback(async () => {
    setLoading(true);
    try {
      await clearDictionary(clientId);
      setWords([]);
    } finally {
      setLoading(false);
    }
  }, [clientId]);

  return { words, loading, error, addWord, removeWord, clear, reload: load };
}
```

### "Add to Dictionary" Button (on match suggestion)

```tsx
// src/components/MatchCard.tsx
import { addDictionaryWord } from '@/lib/proofApi';
import type { Match } from '@/types/proof';

interface Props {
  match: Match;
  text: string;
  clientId: string;
  onAddedToDictionary: (word: string) => void;
}

export function MatchCard({ match, text, clientId, onAddedToDictionary }: Props) {
  const flaggedWord = text.slice(match.offset, match.offset + match.length);
  const isSpelling  = match.rule.issueType === 'misspelling';

  const handleAddToDictionary = async () => {
    await addDictionaryWord(clientId, flaggedWord);
    onAddedToDictionary(flaggedWord);
  };

  return (
    <div style={{ borderLeft: '3px solid red', padding: '8px 12px', marginTop: 8 }}>
      <strong>"{flaggedWord}"</strong> — {match.message}

      <div style={{ marginTop: 4, display: 'flex', gap: 8 }}>
        {match.replacements.slice(0, 4).map(r => (
          <button key={r.value} style={{ fontWeight: 'bold' }}>{r.value}</button>
        ))}

        {/* Only offer "Add to Dictionary" for spelling errors */}
        {isSpelling && clientId && (
          <button onClick={handleAddToDictionary} style={{ color: '#6b7280' }}>
            + Add to Dictionary
          </button>
        )}
      </div>
    </div>
  );
}
```

### Dictionary Management Panel

```tsx
// src/components/DictionaryPanel.tsx
import { useDictionary } from '@/hooks/useDictionary';

export function DictionaryPanel({ clientId }: { clientId: string }) {
  const { words, loading, addWord, removeWord, clear } = useDictionary(clientId);

  return (
    <div>
      <h3>My Dictionary ({words.length} words)</h3>

      {words.map(w => (
        <div key={w.word} style={{ display: 'flex', justifyContent: 'space-between', padding: '4px 0' }}>
          <span>{w.word}</span>
          <button onClick={() => removeWord(w.word)}>Remove</button>
        </div>
      ))}

      {words.length > 0 && (
        <button onClick={clear} style={{ color: 'red', marginTop: 8 }}>
          Clear all
        </button>
      )}

      {loading && <p>Saving...</p>}
    </div>
  );
}
```

### Full example — checker with dictionary

```tsx
// src/components/GrammarCheckerWithDictionary.tsx
import { useState } from 'react';
import { useProofCheck } from '@/hooks/useProofCheck';
import { MatchCard } from './MatchCard';

const CLIENT_ID = 'usr_abc123'; // replace with actual user ID

export function GrammarCheckerWithDictionary() {
  const [text, setText] = useState('');
  const { matches, loading, check, reset } = useProofCheck(CLIENT_ID);

  const handleAddedToDictionary = () => {
    // Re-run check so the newly added word is no longer flagged
    check(text);
  };

  return (
    <div>
      <textarea
        value={text}
        onChange={e => { setText(e.target.value); reset(); }}
        rows={6}
        style={{ width: '100%' }}
      />
      <button onClick={() => check(text)} disabled={loading || text.length < 2}>
        {loading ? 'Checking...' : 'Check Grammar'}
      </button>

      {matches.map((match, i) => (
        <MatchCard
          key={i}
          match={match}
          text={text}
          clientId={CLIENT_ID}
          onAddedToDictionary={handleAddedToDictionary}
        />
      ))}
    </div>
  );
}
```

### Dictionary API Reference

| Method | Endpoint | Header required |
| ------ | -------- | --------------- |
| `POST` | `/v1/dictionary/words` | `X-Client-ID` |
| `GET` | `/v1/dictionary/words` | `X-Client-ID` |
| `DELETE` | `/v1/dictionary/words/{word}` | `X-Client-ID` |
| `DELETE` | `/v1/dictionary` | `X-Client-ID` |

**Request body** for `POST /v1/dictionary/words`:

```json
{ "word": "Tulvo", "language": "en-US" }
```

**Validation rules:**

- Word must be 1–100 characters
- No spaces allowed (single token only)
- `clientId` must be 1–128 alphanumeric chars (`a-z A-Z 0-9 - _ .`)

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

  const check = useCallback((text: string, language = 'en-US', clientId?: string) => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({
      type: 'check',
      text,
      language,
      // omit level/enabledCategories to use server defaults (picky + all categories)
      ...(clientId ? { clientId } : {}),
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
| Per-user dictionary filtering | Pass `clientId` to `useProofCheck` or WS `check()` |
| Add word to dictionary | `addDictionaryWord(clientId, word)` or `useDictionary.addWord()` |
| Show "Add to Dictionary" button | `MatchCard` with `isSpelling` guard |
| List / manage dictionary words | `useDictionary(clientId)` hook |
| List available languages | `GET /v1/languages` |
| Monitor API health | `GET /v1/health` |
| Explore API interactively | Swagger UI at `/docs/index.html` |
