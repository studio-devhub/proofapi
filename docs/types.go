// Package docs contains shared Swagger response type definitions.
package docs

// ErrorResponse is returned on 4xx/5xx errors.
type ErrorResponse struct {
	Error string `json:"error" example:"unauthorized"`
}

// ClearCacheResponse is returned by DELETE /v1/cache.
type ClearCacheResponse struct {
	Deleted int64 `json:"deleted" example:"42"`
}

// LanguageItem represents a single supported language.
type LanguageItem struct {
	Code     string `json:"code"     example:"en-US"`
	LongCode string `json:"longCode" example:"en-US"`
	Name     string `json:"name"     example:"English (US)"`
}

// CacheStats holds Redis cache metrics returned in the health response.
type CacheStats struct {
	Hits       int64  `json:"hits"       example:"120"`
	Misses     int64  `json:"misses"     example:"30"`
	Keys       int64  `json:"keys"       example:"15"`
	MemoryUsed string `json:"memoryUsed" example:"1.19M"`
}

// WebSocketStats holds the active/total WebSocket connection counts.
type WebSocketStats struct {
	Active int64 `json:"active" example:"3"`
	Total  int64 `json:"total"  example:"47"`
}

// HealthResponse is returned by GET /v1/health.
type HealthResponse struct {
	API          string         `json:"api"          example:"ok"`
	LanguageTool string         `json:"languagetool" example:"ok"`
	Redis        string         `json:"redis"        example:"ok"`
	WebSocket    WebSocketStats `json:"websocket"`
	CacheStats   CacheStats     `json:"cacheStats"`
}

// DictionaryWord represents a single word in the user's custom dictionary.
type DictionaryWord struct {
	Word     string `json:"word"               example:"Tulvo"`
	Language string `json:"language,omitempty" example:"en-US"`
	AddedAt  string `json:"addedAt"            example:"2026-05-20T10:00:00Z"`
}

// DictionaryListResponse is returned by GET /v1/dictionary/words.
type DictionaryListResponse struct {
	ClientID string           `json:"clientId" example:"dev-client-1"`
	Words    []DictionaryWord `json:"words"`
	Count    int              `json:"count"    example:"2"`
}

// DictionaryAddRequest is the body for POST /v1/dictionary/words.
type DictionaryAddRequest struct {
	Word     string `json:"word"               example:"Tulvo"`
	Language string `json:"language,omitempty" example:"en-US"`
}

// DictionaryRemoveResponse is returned by DELETE /v1/dictionary/words/{word}.
type DictionaryRemoveResponse struct {
	Removed string `json:"removed" example:"Tulvo"`
}

// DictionaryClearResponse is returned by DELETE /v1/dictionary.
type DictionaryClearResponse struct {
	Cleared  bool   `json:"cleared"   example:"true"`
	ClientID string `json:"clientId"  example:"dev-client-1"`
}
