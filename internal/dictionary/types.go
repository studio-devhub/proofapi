package dictionary

import "time"

type Word struct {
	Word     string    `json:"word"`
	Language string    `json:"language,omitempty"`
	AddedAt  time.Time `json:"addedAt"`
}

type AddWordRequest struct {
	Word     string `json:"word"`
	Language string `json:"language,omitempty"`
}

type ListResponse struct {
	ClientID string `json:"clientId"`
	Words    []Word `json:"words"`
	Count    int    `json:"count"`
}

// ValidationError signals bad user input — mapped to HTTP 400.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }
