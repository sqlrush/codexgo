package websearch

import _ "embed"

// webRunDescription is the model-facing web.run tool description. Rust:
// WEB_RUN_DESCRIPTION (include_str!("../web_run_description.md")).
//
//go:embed web_run_description.md
var webRunDescription string

// commandsSchema is the tool input schema produced for SearchCommands. The Rust
// crate generates the root schema with SchemaSettings::draft2019_09,
// inline_subschemas=true, and option_add_null_type=false, then copies only the
// "properties", "required", "type", "additionalProperties", "$defs", and
// "definitions" keys into the tool schema. SearchCommands has no
// deny_unknown_fields and no required fields, so only "type" and "properties"
// are present after extraction. Each field's Rust doc comment becomes its
// "description"; every command field is an inlined array of its operation type.
//
// This mirrors the schemars output field-for-field so the parsed tool definition
// matches the hosted tool wire form.
const commandsSchema = `{
  "type": "object",
  "properties": {
    "search_query": {
      "description": "Query the internet search engine for a given list of queries.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "q": {
            "description": "Search query.",
            "type": "string"
          },
          "recency": {
            "description": "Whether to filter by recency, as a number of recent days.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          },
          "domains": {
            "description": "Whether to filter by a specific list of domains.",
            "type": "array",
            "items": {
              "type": "string"
            }
          }
        },
        "required": ["q"]
      }
    },
    "image_query": {
      "description": "Query the image search engine for a given list of queries.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "q": {
            "description": "Search query.",
            "type": "string"
          },
          "recency": {
            "description": "Whether to filter by recency, as a number of recent days.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          },
          "domains": {
            "description": "Whether to filter by a specific list of domains.",
            "type": "array",
            "items": {
              "type": "string"
            }
          }
        },
        "required": ["q"]
      }
    },
    "open": {
      "description": "Open pages by reference id or URL.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ref_id": {
            "description": "Reference id or URL to open.",
            "type": "string"
          },
          "lineno": {
            "description": "Line number to position the page at.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          }
        },
        "required": ["ref_id"]
      }
    },
    "click": {
      "description": "Open links from previously opened pages.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ref_id": {
            "description": "Reference id containing the numbered link.",
            "type": "string"
          },
          "id": {
            "description": "Numbered link id to open.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          }
        },
        "required": ["ref_id", "id"]
      }
    },
    "find": {
      "description": "Find text patterns in pages.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ref_id": {
            "description": "Reference id or URL to search within.",
            "type": "string"
          },
          "pattern": {
            "description": "Text pattern to find.",
            "type": "string"
          }
        },
        "required": ["ref_id", "pattern"]
      }
    },
    "screenshot": {
      "description": "Take screenshots of PDF pages.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ref_id": {
            "description": "Reference id or URL to screenshot.",
            "type": "string"
          },
          "pageno": {
            "description": "Zero-indexed PDF page number.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          }
        },
        "required": ["ref_id", "pageno"]
      }
    },
    "finance": {
      "description": "Look up prices for the given stock symbols.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ticker": {
            "description": "Ticker symbol to look up.",
            "type": "string"
          },
          "type": {
            "description": "Asset type to look up.",
            "type": "string",
            "enum": ["equity", "fund", "crypto", "index"]
          },
          "market": {
            "description": "ISO 3166-1 alpha-3 country code, \"OTC\", or \"\" for cryptocurrency.",
            "type": "string"
          }
        },
        "required": ["ticker", "type"]
      }
    },
    "weather": {
      "description": "Look up weather forecasts.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "location": {
            "description": "Location in \"Country, Area, City\" format.",
            "type": "string"
          },
          "start": {
            "description": "Start date in YYYY-MM-DD format. Defaults to today.",
            "type": "string"
          },
          "duration": {
            "description": "Number of days to return. Defaults to 7.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          }
        },
        "required": ["location"]
      }
    },
    "sports": {
      "description": "Look up sports schedules and standings.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "tool": {
            "description": "Tool name for sports requests.",
            "type": "string",
            "enum": ["sports"]
          },
          "fn": {
            "description": "Sports function to call.",
            "type": "string",
            "enum": ["schedule", "standings"]
          },
          "league": {
            "description": "League to look up.",
            "type": "string",
            "enum": ["nba", "wnba", "nfl", "nhl", "mlb", "epl", "ncaamb", "ncaawb", "ipl"]
          },
          "team": {
            "description": "Team to look up, using the common 3 or 4 letter alias used in broadcasts.",
            "type": "string"
          },
          "opponent": {
            "description": "Opponent to use with ` + "`team`" + ` when narrowing the lookup.",
            "type": "string"
          },
          "date_from": {
            "description": "Start date in YYYY-MM-DD format.",
            "type": "string"
          },
          "date_to": {
            "description": "End date in YYYY-MM-DD format.",
            "type": "string"
          },
          "num_games": {
            "description": "Number of games to return.",
            "type": "integer",
            "format": "uint64",
            "minimum": 0.0
          },
          "locale": {
            "description": "Locale for the lookup.",
            "type": "string"
          }
        },
        "required": ["fn", "league"]
      }
    },
    "time": {
      "description": "Get time for the given UTC offsets.",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "utc_offset": {
            "description": "UTC offset formatted like \"+03:00\".",
            "type": "string"
          }
        },
        "required": ["utc_offset"]
      }
    },
    "response_length": {
      "description": "Set the length of the response to be returned.",
      "type": "string",
      "enum": ["short", "medium", "long"]
    }
  }
}`
