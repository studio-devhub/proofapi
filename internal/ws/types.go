package ws

import "languagetool-backend/internal/languagetool"

// ── Incoming (Client → Server) ────────────────────────────

type IncomingMessage struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
	SeqID    int    `json:"seqId"` // client sequence — for ordering
}

// ── Outgoing (Server → Client) ────────────────────────────

type OutgoingMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
	SeqID   int         `json:"seqId"`
	Error   string      `json:"error,omitempty"`
}

type CheckPayload struct {
	Matches  []languagetool.Match  `json:"matches"`
	Language languagetool.Language `json:"language"`
	Cached   bool                  `json:"cached"`
	LatencyMs int64                `json:"latencyMs"`
}

// Message types
const (
	TypeCheck   = "check"    // client sends text to check
	TypeResult  = "result"   // server sends corrections
	TypeError   = "error"    // server sends error
	TypePing    = "ping"     // client keepalive
	TypePong    = "pong"     // server keepalive response
	TypeAck     = "ack"      // server acknowledges connection
)
