package services

import (
	"fmt"
	"io"
)

const (
	// maxAPIResponseSize is the limit for large responses (polygon/GeoJSON/boundary data).
	// Real-world OSM boundary polygons for complex regions can reach 5-15 MB,
	// so 50 MB provides ample headroom while preventing multi-GB OOM attacks.
	maxAPIResponseSize = 50 * 1024 * 1024 // 50 MB

	// maxSmallResponseSize is the limit for small API responses (geocoding, address validation, postcards).
	// These responses rarely exceed a few KB, so 5 MB is generous.
	maxSmallResponseSize = 5 * 1024 * 1024 // 5 MB
)

// readLimitedBody reads up to limit bytes from r. If the response exceeds the
// limit, it returns an error instead of consuming unbounded memory.
func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	limitedReader := io.LimitReader(r, limit+1) // +1 to detect exceeding
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d byte limit", limit)
	}
	return data, nil
}
