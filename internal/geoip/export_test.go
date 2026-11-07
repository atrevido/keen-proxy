package geoip

// SetMaxDatabase lowers the decompression limit for a test and returns a restore function.
func SetMaxDatabase(n int64) (restore func()) {
	old := maxDatabase
	maxDatabase = n
	return func() { maxDatabase = old }
}
