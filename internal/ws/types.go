package ws

import "languagetool-backend/internal/languagetool"

// ── Incoming (Client → Server) ────────────────────────────

type IncomingMessage struct {
	Type               string `json:"type"`
	Text               string `json:"text"`
	Language           string `json:"language,omitempty"`
	ClientID             string `json:"clientId,omitempty"`
	Level              string `json:"level,omitempty"`
	MotherTongue       string `json:"motherTongue,omitempty"`
	EnabledCategories  string `json:"enabledCategories,omitempty"`
	DisabledCategories string `json:"disabledCategories,omitempty"`
	EnabledRules       string `json:"enabledRules,omitempty"`
	DisabledRules      string `json:"disabledRules,omitempty"`
	SeqID              int    `json:"seqId"`
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
