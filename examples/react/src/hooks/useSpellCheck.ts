import { useEffect, useRef, useCallback, useState } from "react";
import type { Match, SpellCheckResult } from "../types/proof";

type Status = "connecting" | "connected" | "disconnected" | "error";

interface UseSpellCheck {
  check: (text: string, language?: string) => void;
  result: SpellCheckResult | null;
  status: Status;
  clearMatches: () => void;
}

const WS_URL  = import.meta.env.VITE_PROOF_WS_URL  ?? "ws://localhost:4003/v1/ws";
const API_KEY = import.meta.env.VITE_PROOF_API_KEY ?? "";

export function useSpellCheck(): UseSpellCheck {
  const wsRef        = useRef<WebSocket | null>(null);
  const seqRef       = useRef(0);
  const reconnectRef = useRef<ReturnType<typeof setTimeout>>();

  const [result, setResult] = useState<SpellCheckResult | null>(null);
  const [status, setStatus] = useState<Status>("connecting");

  const connect = useCallback(() => {
    wsRef.current?.close();
    const ws = new WebSocket(`${WS_URL}?api_key=${API_KEY}`);
    wsRef.current = ws;

    ws.onopen    = () => { setStatus("connected"); clearTimeout(reconnectRef.current); };
    ws.onclose   = () => { setStatus("disconnected"); reconnectRef.current = setTimeout(connect, 2000); };
    ws.onerror   = () => { setStatus("error"); ws.close(); };
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      if (msg.type === "result" && msg.payload) {
        setResult({
          matches:   msg.payload.matches  ?? [],
          cached:    msg.payload.cached   ?? false,
          latencyMs: msg.payload.latencyMs ?? 0,
        });
      }
    };
  }, []);

  useEffect(() => {
    connect();
    return () => { clearTimeout(reconnectRef.current); wsRef.current?.close(); };
  }, [connect]);

  const check = useCallback((text: string, language = "en-US") => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({
      type: "check", text, language, seqId: ++seqRef.current,
    }));
  }, []);

  const clearMatches = useCallback(() => setResult(null), []);

  return { check, result, status, clearMatches };
}
