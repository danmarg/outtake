package lib

// Progress represents a simple "done xxx out of yyy"-style progress report.
type Progress struct {
	Current uint
	Total   uint
}
