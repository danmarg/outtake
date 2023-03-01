package lib

// Progress represents a simple "done xxx out of yyy"-style progress report.
type Progress struct {
	Current int64
	Total   int64
}
