package languagetool

type CheckRequest struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	// "default" | "picky" — picky enables extra style/punctuation rules
	Level string `json:"level"`
	// BCP 47 code of the user's native language — enables false-friends detection (e.g. "de-DE" for a German speaker writing English)
	MotherTongue string `json:"motherTongue,omitempty"`
	// Comma-separated rule IDs to enable on top of the active profile
	EnabledRules string `json:"enabledRules,omitempty"`
	// Comma-separated rule IDs to suppress
	DisabledRules string `json:"disabledRules,omitempty"`
	// Comma-separated category IDs to enable (e.g. "STYLE,PUNCTUATION")
	EnabledCategories string `json:"enabledCategories,omitempty"`
	// Comma-separated category IDs to suppress
	DisabledCategories string `json:"disabledCategories,omitempty"`
	// When true, only the rules/categories in EnabledRules/EnabledCategories run
	EnabledOnly bool `json:"enabledOnly,omitempty"`
}

type CheckResponse struct {
	Matches   []Match  `json:"matches"`
	Language  Language `json:"language"`
	CheckedAt string   `json:"checkedAt"`
	Cached    bool     `json:"cached"`
	ExpiresIn int      `json:"cacheExpiresIn,omitempty"`
}

type Match struct {
	Message      string        `json:"message"`
	Offset       int           `json:"offset"`
	Length       int           `json:"length"`
	Replacements []Replacement `json:"replacements"`
	Rule         Rule          `json:"rule"`
	Context      Context       `json:"context"`
}

type Replacement struct {
	Value string `json:"value"`
}

type Rule struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	IssueType   string   `json:"issueType"`
	Category    Category `json:"category"`
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Context struct {
	Text   string `json:"text"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type Language struct {
	Name string `json:"name"`
	Code string `json:"code"`
}
