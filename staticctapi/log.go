package staticctapi

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"filippo.io/sunlight"
	"github.com/cenkalti/backoff/v4"
)

// TilePathFromIndex converts an integer index to a tile path string.
func TilePathFromIndex(tileIndex int64) string {
	path := fmt.Sprintf("%03d", tileIndex%1000)
	remainder := tileIndex / 1000

	for remainder != 0 {
		path = fmt.Sprintf("x%03d/%s", remainder%1000, path)
		remainder = remainder / 1000
	}

	return path
}

// TreeSizeFromCheckpoint verifies the given checkpoint is parseable, then
// returns the parsed tree size.
func TreeSizeFromCheckpoint(text string) (int64, error) {
	if strings.Count(text, "\n") < 3 || len(text) > 1e6 {
		return -1, errors.New("malformed checkpoint: incorrect size")
	}

	lines := strings.SplitN(text, "\n", 4)

	treeSize, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil || treeSize < 0 || lines[1] != strconv.FormatInt(treeSize, 10) {
		return -1, errors.New("malformed checkpoint: invalid tree size")
	}

	hash, err := base64.StdEncoding.DecodeString(lines[2])
	if err != nil || len(hash) != 32 {
		return -1, errors.New("malformed checkpoint: invalid root hash")
	}

	return treeSize, nil
}

// Log represents a tiled CT log implementing the Static CT API spec.
type Log struct {
	httpClient *http.Client

	// MetricsEndpoint is the URL for the metrics endpoint of the log, as
	// defined by the Static CT API specification.
	MetricsEndpoint *url.URL

	// TileRetry describes the retry behavior to be used by
	// GetTileEntriesWithBackoff. If TileRetry is the empty value,
	// DefaultTileRetry is used.
	TileRetry Retry
}

func NewLog(metricsEndpoint string) (*Log, error) {
	endpointUrl, err := url.Parse(metricsEndpoint)
	if err != nil {
		return nil, err
	}

	log := &Log{
		httpClient:      &http.Client{},
		MetricsEndpoint: endpointUrl,
	}
	return log, nil
}

// GetTileEntries fetches the data tile at the given index and parses the
// entries from it.
func (l *Log) GetTileEntries(ctx context.Context, tileIndex int64) ([]*sunlight.LogEntry, error) {
	tilePath := fmt.Sprintf("/tile/data/%s", TilePathFromIndex(tileIndex))
	tileUrl := l.MetricsEndpoint.JoinPath(tilePath).String()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tileUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("building http request: %w", err)
	}

	request.Header.Add("Accept-Encoding", "gzip, identity")

	response, err := l.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("requesting tile: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected response status: %s", response.Status)
	}

	var tileData []byte

	// Tile data may be gzip-compressed
	if strings.HasPrefix(response.Header.Get("Content-Encoding"), "gzip") {
		reader, err := gzip.NewReader(response.Body)
		if err != nil {
			return nil, fmt.Errorf("creating gzip reader: %w", err)
		}

		defer reader.Close()

		tileData, err = io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("reading data from gzipped response body: %w", err)
		}
	} else {
		tileData, err = io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response body: %w", err)
		}
	}

	entries := make([]*sunlight.LogEntry, 256)

	for entryIndex := 0; entryIndex < 256; entryIndex++ {
		entry, rest, err := sunlight.ReadTileLeaf(tileData)
		if err != nil {
			return nil, fmt.Errorf("reading entry from tile: %w", err)
		}

		entries[entryIndex] = entry
		tileData = rest
	}

	return entries, nil
}

// GetTileEntriesWithBackoff fetches the data tile at the given index and parses
// the entries from it, retrying the request upon failure according to the
// settings in TileRetry.
func (l *Log) GetTileEntriesWithBackoff(ctx context.Context, tileIndex int64) ([]*sunlight.LogEntry, error) {
	bo := DefaultTileRetry.createBackoff()
	if l.TileRetry.Validate() == nil {
		bo = l.TileRetry.createBackoff()
	}

	var operation backoff.OperationWithData[[]*sunlight.LogEntry] = func() ([]*sunlight.LogEntry, error) {
		return l.GetTileEntries(ctx, tileIndex)
	}

	return backoff.RetryWithData(operation, backoff.WithContext(bo, ctx))
}

// GetLastFullTileIndex returns the index of the last full tile currently
// available in the log.
func (l *Log) GetLastFullTileIndex(ctx context.Context) (int64, error) {
	checkpointUrl := l.MetricsEndpoint.JoinPath("/checkpoint").String()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checkpointUrl, nil)
	if err != nil {
		return -1, fmt.Errorf("building http request: %w", err)
	}

	response, err := l.httpClient.Do(request)
	if err != nil {
		return -1, fmt.Errorf("requesting checkpoint: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode != 200 {
		return -1, fmt.Errorf("unexpected response status: %s", response.Status)
	}

	checkpointData, err := io.ReadAll(response.Body)
	if err != nil {
		return -1, fmt.Errorf("reading response body: %w", err)
	}

	treeSize, err := TreeSizeFromCheckpoint(string(checkpointData))
	if err != nil {
		return -1, fmt.Errorf("parsing tree size from checkpoint: %w", err)
	}

	return treeSize / 256, nil
}

// GetTileIndexFromTime performs a binary search against the log to find the
// index of the data tile containing the given timestamp. The search is bounded
// between startTile and endTile. This method takes advantage of the fact that
// in practice, logs implementing the Static CT API store their entries in
// sequential order.
func (l *Log) GetTileIndexFromTime(ctx context.Context, t time.Time, startTile int64, endTile int64) (int64, error) {
	if startTile < 0 {
		return -1, errors.New("negative startTile")
	}

	startIndex := startTile
	endIndex := endTile
	for startIndex <= endIndex {
		pivotIndex := (startIndex + endIndex) / 2
		tileEntries, err := l.GetTileEntries(ctx, pivotIndex)
		if err != nil {
			return -1, fmt.Errorf("getting entries for tile: %w", err)
		}

		firstTime := time.UnixMilli(tileEntries[0].Timestamp)
		if t.Before(firstTime) {
			endIndex = pivotIndex - 1
			continue
		}

		lastTime := time.UnixMilli(tileEntries[255].Timestamp)
		if t.After(lastTime) {
			startIndex = pivotIndex + 1
			continue
		}

		return pivotIndex, nil
	}

	return -1, errors.New("timestamp doesn't fall within the time bounds of the log entries")
}

// GetBoundingTilesFromTimes finds the indexes of the data tiles bounding the
// timespan described by startTime and endTime.
func (l *Log) GetBoundingTilesFromTimes(ctx context.Context, startTime time.Time, endTime time.Time) (int64, int64, error) {
	if !startTime.Before(endTime) {
		return -1, -1, errors.New("start time is not before end time")
	}

	lastTile, err := l.GetLastFullTileIndex(ctx)
	if err != nil {
		return -1, -1, fmt.Errorf("getting index of current final tile: %w", err)
	}

	startIndex, err := l.GetTileIndexFromTime(ctx, startTime, 0, lastTile)
	if err != nil {
		return -1, -1, fmt.Errorf("getting index of start tile: %w", err)
	}

	// Use the index that was already found to bound the next search
	endIndex, err := l.GetTileIndexFromTime(ctx, endTime, startIndex, lastTile)
	if err != nil {
		return -1, -1, fmt.Errorf("getting index of end tile: %w", err)
	}

	return startIndex, endIndex, nil
}
