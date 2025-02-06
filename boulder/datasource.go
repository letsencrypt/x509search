package boulder

import (
	"context"
	"errors"
	"time"
)

type DataSource struct {
	// DB is a handler to Boulder's database.
	DB *Database

	// IncludePrecertificates causes precertificates to be included in the
	// output of this data source.
	// TODO: Figure out the best way to implement precertificates.
	// We only write the linting precert, not the real precert, to the database.
	IncludePrecertificates bool

	// IncludePrecertificates causes final certificates to be included in the
	// output of this data source.
	IncludeCertificates bool

	// StartTimeInclusive is the start of the search window. Certificates issued
	// before this time will be ignored.
	StartTimeInclusive time.Time

	// EndTimeInclusive is the end of the search window. Certificates issued
	// after this time will be ignored.
	EndTimeInclusive time.Time

	// MaxConnections is the maximum number of concurrent connections that will
	// be used to download certificate data from Boulder's database.
	MaxConnections int

	// CertificateBatchSize is the maximum number of certificates that will be
	// downloaded in a single database query.
	CertificateBatchSize int

	// PrecertificateBatchSize is the maximum number of precertificates that
	// will be downloaded in a single database query.
	PrecertificateBatchSize int
}

func (d *DataSource) Source(ctx context.Context, certs chan<- []byte) error {
	// TODO: Implement Source()
	return errors.New("not implemented")
}
