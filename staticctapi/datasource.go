package staticctapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type DataSource struct {
	// Log is the tiled log that should be searched.
	Log *Log

	// IncludePrecertificates causes precertificates to be included in the
	// output of this data source.
	IncludePrecertificates bool

	// IncludePrecertificates causes final certificates to be included in the
	// output of this data source.
	IncludeCertificates bool

	// StartTimeInclusive is the timestamp used to determine the starting data
	// tile for the search. It must fall within the timespan that the log was
	// accepting entries (not the submission window, which is the timespan
	// describing the notAfter timestamps accepted by a temporally-sharded log).
	StartTimeInclusive time.Time

	// EndTimeInclusive is the timestamp used to determine the ending data tile
	// for the search. It must fall within the timespan that the log was
	// accepting entries (not the submission window, which is the timespan
	// describing the notAfter timestamps accepted by a temporally-sharded log).
	EndTimeInclusive time.Time

	// MaxConnections is the number of concurrent requests that should be used
	// to download data tiles from the log. If MaxConnections is less than 1,
	// then the requests are made sequentially.
	MaxConnections int
}

func (b DataSource) Source(ctx context.Context, certs chan<- []byte) error {
	if b.Log == nil {
		return errors.New("nil log")
	}

	if !(b.IncludeCertificates || b.IncludePrecertificates) {
		return errors.New("neither precertficates nor certificates are selected")
	}

	concurrency := 1
	if b.MaxConnections > 1 {
		concurrency = b.MaxConnections
	}

	startIndex, endIndex, err := b.Log.GetBoundingTilesFromTimes(ctx, b.StartTimeInclusive, b.EndTimeInclusive)
	if err != nil {
		return fmt.Errorf("determining search bounds: %w", err)
	}

	fmt.Fprintf(os.Stderr, "determined search bounds, start tile: %d end tile: %d\n", startIndex, endIndex)

	var wg sync.WaitGroup
	workChan := make(chan int64, concurrency)

	go func(ch chan<- int64) {
		for currentIndex := startIndex; currentIndex <= endIndex; currentIndex++ {
			ch <- currentIndex
		}
		close(ch)
	}(workChan)

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tileIndex := range workChan {
				entries, err := b.Log.GetTileEntriesWithBackoff(ctx, tileIndex)
				if err != nil {
					fmt.Fprintf(os.Stderr, "getting entries for tile: %s\n", err.Error())
					continue
				}

				for _, entry := range entries {
					if entry.IsPrecert {
						if b.IncludePrecertificates {
							certs <- entry.PreCertificate
						}
						continue
					}
					if b.IncludeCertificates {
						certs <- entry.Certificate
					}
				}
			}
		}()
	}

	wg.Wait()
	return nil
}
