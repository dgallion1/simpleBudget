package models

// Health classifies a page's verdict-band status and drives the shared
// verdict-band tinting (see verdictBandClass / verdictLabelClass in
// internal/templates/render.go). It prints as its string value, so templates
// can keep emitting `verdict-{{.Health}}` CSS hook classes.
type Health string

const (
	HealthGreen   Health = "green"
	HealthAmber   Health = "amber"
	HealthRed     Health = "red"
	HealthNeutral Health = "neutral"
)
