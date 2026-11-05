package ir

// InputMode is how input reaches a session. The server selects the mode.
// The driver maps it to a native command, or the daemon emulates it.
type InputMode string

const (
	ModePrompt    InputMode = "prompt"    // start a turn; only when idle
	ModeSteer     InputMode = "steer"     // add to the running turn, before the next model call
	ModeFollowUp  InputMode = "follow_up" // run after the current turn ends
	ModeNote      InputMode = "note"      // add to context, do not start a turn
	ModeInterrupt InputMode = "interrupt" // stop the running turn
)

// Input is one delivery to a session.
type Input struct {
	Mode   InputMode `json:"mode"`
	Text   string    `json:"text,omitempty"`
	Images []Image   `json:"images,omitempty"`
	Ref    string    `json:"ref"` // Solax batch ID, for idempotency
}

// Image is an image attached to an input.
type Image struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

// Caps describes what a driver supports.
type Caps struct {
	// Native maps each input mode to true when the agent supports it, and
	// to false when the daemon emulates it.
	Native   map[InputMode]bool `json:"native"`
	Requests bool               `json:"requests"` // can raise approval requests
	Deltas   bool               `json:"deltas"`   // streams text deltas
	Subagent bool               `json:"subagent"` // emits nested runs
}
