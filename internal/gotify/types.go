// Package gotify holds the wire-compatible types and helpers that mirror the
// upstream Gotify server (https://gotify.net/api-docs). Keeping these shapes
// identical is what lets the stock Gotify senders and Android app talk to this
// serverless backend unmodified.
package gotify

// Application is a source of messages. Senders authenticate with its Token
// (prefix "A"). Mirrors Gotify's Application model.
type Application struct {
	ID              int64   `json:"id"`
	Token           string  `json:"token"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Internal        bool    `json:"internal"`
	Image           string  `json:"image"`
	DefaultPriority int     `json:"defaultPriority"`
	LastUsed        *string `json:"lastUsed"`
}

// Client is a message receiver (e.g. the Android app). It authenticates with
// its Token (prefix "C") for both REST and the WebSocket stream.
type Client struct {
	ID       int64   `json:"id"`
	Token    string  `json:"token"`
	Name     string  `json:"name"`
	LastUsed *string `json:"lastUsed"`
}

// Message is a single notification. The exact JSON shape is what gets pushed
// over the WebSocket and returned by the REST API.
type Message struct {
	ID            int64                  `json:"id"`
	ApplicationID int64                  `json:"appid"`
	Message       string                 `json:"message"`
	Title         string                 `json:"title"`
	Priority      int                    `json:"priority"`
	Date          string                 `json:"date"`
	Extras        map[string]interface{} `json:"extras,omitempty"`
}

// PagedMessages is the envelope returned by the message listing endpoints.
type PagedMessages struct {
	Messages []Message `json:"messages"`
	Paging   Paging    `json:"paging"`
}

// Paging mirrors Gotify's paging contract: messages are returned in descending
// id order and Since/Next form the cursor to the next (older) page.
type Paging struct {
	Next  string `json:"next,omitempty"`
	Since int64  `json:"since,omitempty"`
	Size  int    `json:"size"`
	Limit int    `json:"limit"`
}

// User is returned by GET /current/user. This is a single-user deployment, so
// it is always the configured admin.
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
}

// VersionInfo is returned by GET /version; the Android app reads it to gate
// features, so Version must look like a real Gotify version.
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// Health is returned by GET /health.
type Health struct {
	Health   string `json:"health"`
	Database string `json:"database"`
}

// Error mirrors Gotify's error envelope.
type Error struct {
	Error            string `json:"error"`
	ErrorCode        int    `json:"errorCode"`
	ErrorDescription string `json:"errorDescription"`
}
