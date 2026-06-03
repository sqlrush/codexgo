package websearch

// SearchCommands is the set of operations the model can request in one web.run
// call. Rust: SearchCommands; every field uses skip_serializing_if =
// Option::is_none, so absent commands are omitted. All keys are snake_case.
type SearchCommands struct {
	SearchQuery    *[]SearchQuery        `json:"search_query,omitempty"`
	ImageQuery     *[]SearchQuery        `json:"image_query,omitempty"`
	Open           *[]OpenOperation      `json:"open,omitempty"`
	Click          *[]ClickOperation     `json:"click,omitempty"`
	Find           *[]FindOperation      `json:"find,omitempty"`
	Screenshot     *[]ScreenshotOp       `json:"screenshot,omitempty"`
	Finance        *[]FinanceOperation   `json:"finance,omitempty"`
	Weather        *[]WeatherOperation   `json:"weather,omitempty"`
	Sports         *[]SportsOperation    `json:"sports,omitempty"`
	Time           *[]TimeOperation      `json:"time,omitempty"`
	ResponseLength *SearchResponseLength `json:"response_length,omitempty"`
}

// SearchQuery is one search/image query. Rust: SearchQuery.
type SearchQuery struct {
	Q       string    `json:"q"`
	Recency *uint64   `json:"recency,omitempty"`
	Domains *[]string `json:"domains,omitempty"`
}

// OpenOperation opens a page by reference id or URL. Rust: OpenOperation.
type OpenOperation struct {
	RefID  string  `json:"ref_id"`
	Lineno *uint64 `json:"lineno,omitempty"`
}

// ClickOperation clicks a numbered link in a prior page. Rust: ClickOperation.
type ClickOperation struct {
	RefID string `json:"ref_id"`
	ID    uint64 `json:"id"`
}

// FindOperation finds a text pattern within a page. Rust: FindOperation.
type FindOperation struct {
	RefID   string `json:"ref_id"`
	Pattern string `json:"pattern"`
}

// ScreenshotOp screenshots a PDF page. Rust: ScreenshotOperation.
type ScreenshotOp struct {
	RefID  string `json:"ref_id"`
	Pageno uint64 `json:"pageno"`
}

// FinanceOperation looks up a financial instrument. Rust: FinanceOperation.
type FinanceOperation struct {
	Ticker string           `json:"ticker"`
	Type   FinanceAssetType `json:"type"`
	Market *string          `json:"market,omitempty"`
}

// FinanceAssetType is a financial asset type. Rust: FinanceAssetType (lowercase).
type FinanceAssetType string

// FinanceAssetType variants.
const (
	FinanceAssetTypeEquity FinanceAssetType = "equity"
	FinanceAssetTypeFund   FinanceAssetType = "fund"
	FinanceAssetTypeCrypto FinanceAssetType = "crypto"
	FinanceAssetTypeIndex  FinanceAssetType = "index"
)

// WeatherOperation looks up a weather forecast. Rust: WeatherOperation.
type WeatherOperation struct {
	Location string  `json:"location"`
	Start    *string `json:"start,omitempty"`
	Duration *uint64 `json:"duration,omitempty"`
}

// SportsOperation looks up sports schedules/standings. Rust: SportsOperation.
type SportsOperation struct {
	Tool     *SportsToolName `json:"tool,omitempty"`
	Fn       SportsFunction  `json:"fn"`
	League   SportsLeague    `json:"league"`
	Team     *string         `json:"team,omitempty"`
	Opponent *string         `json:"opponent,omitempty"`
	DateFrom *string         `json:"date_from,omitempty"`
	DateTo   *string         `json:"date_to,omitempty"`
	NumGames *uint64         `json:"num_games,omitempty"`
	Locale   *string         `json:"locale,omitempty"`
}

// SportsToolName is the sports tool name. Rust: SportsToolName (lowercase).
type SportsToolName string

// SportsToolName variants.
const (
	SportsToolNameSports SportsToolName = "sports"
)

// SportsFunction is a sports function. Rust: SportsFunction (lowercase).
type SportsFunction string

// SportsFunction variants.
const (
	SportsFunctionSchedule  SportsFunction = "schedule"
	SportsFunctionStandings SportsFunction = "standings"
)

// SportsLeague is a sports league. Rust: SportsLeague (lowercase).
type SportsLeague string

// SportsLeague variants.
const (
	SportsLeagueNba    SportsLeague = "nba"
	SportsLeagueWnba   SportsLeague = "wnba"
	SportsLeagueNfl    SportsLeague = "nfl"
	SportsLeagueNhl    SportsLeague = "nhl"
	SportsLeagueMlb    SportsLeague = "mlb"
	SportsLeagueEpl    SportsLeague = "epl"
	SportsLeagueNcaamb SportsLeague = "ncaamb"
	SportsLeagueNcaawb SportsLeague = "ncaawb"
	SportsLeagueIpl    SportsLeague = "ipl"
)

// TimeOperation looks up the time for a UTC offset. Rust: TimeOperation.
type TimeOperation struct {
	UTCOffset string `json:"utc_offset"`
}

// SearchResponseLength sets the desired response length. Rust:
// SearchResponseLength (lowercase).
type SearchResponseLength string

// SearchResponseLength variants.
const (
	SearchResponseLengthShort  SearchResponseLength = "short"
	SearchResponseLengthMedium SearchResponseLength = "medium"
	SearchResponseLengthLong   SearchResponseLength = "long"
)
