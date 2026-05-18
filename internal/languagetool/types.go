package languagetool

type CheckRequest struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	Level    string `json:"level"`
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
