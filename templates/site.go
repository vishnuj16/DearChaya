package templates

// Site carries the settings and viewer state the templates need.
type Site struct {
	// GAMeasurementID enables the GA4 tag when non-empty (e.g. "G-ABC123").
	GAMeasurementID string
	// Admin is true when the viewer is signed in as the author.
	Admin bool
}
