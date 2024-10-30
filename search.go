package x509search

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
)

type ErrorBehavior int

const (
	// Cancel the search and return the error that caused the cancellation.
	ErrorBehaviorCancel ErrorBehavior = iota

	// Continue the search without the data source that errored.
	ErrorBehaviorContinue
)

// Sourcer is a data source for X.509 certificates.
type Sourcer interface {
	// Source sends all potentially-relevant X.509 certificates over the certs
	// channel in their DER-encoded form. If an unrecoverable error is
	// encountered, it is returned; else, nil is returned once all available
	// certificates have been exhausted. The caller retains the responsibility
	// of closing the certs channel once Source has returned and must not close
	// the channel until it does.
	//
	// If ctx is cancelled before the data source is exhausted, Source returns
	// ctx.Err().
	Source(ctx context.Context, certs chan<- []byte) error
}

// Search is an X.509 certificate search supporting multiple concurrent data
// sources and match de-duplication.
type Search struct {
	// DERFilter should return true if the raw DER bytes that were passed in
	// match the desired search parameters, and false otherwise. It is called
	// for each certificate discovered by one of the configured data sources,
	// and may be called more than once for any given certificate. If DERFilter
	// returns false, the certificate in question will not be parsed or passed
	// to Filter.
	//
	// A single goroutine is responsible for invoking DERFilter, so it is safe
	// to access memory outside of the function scope if desired.
	DERFilter func([]byte) bool

	// Filter should return true if the certificate that was passed in matches
	// the desired search parameters, and false otherwise. It is called for each
	// certificate discovered by one of the configured data sources, and may be
	// called more than once for any given certificate. Filter is only called
	// if DERFilter returns true for the same certificate.
	//
	// A single goroutine is responsible for invoking Filter, so it is safe to
	// access memory outside of the function scope if desired.
	Filter func(*x509.Certificate) bool

	// MatchCallback is called for each certificate matching the search filter
	// that hasn't already been cached by MatchCacher.
	//
	// A single goroutine is responsible for invoking MatchCallback, so it is
	// safe to access memory outside of the function scope if desired.
	MatchCallback func(*x509.Certificate)

	// DataSources contains all the data sources to be used in the search. For
	// each data source, a dedicated goroutine will be created where its Source
	// method will be invoked.
	DataSources []Sourcer

	// MatchCacher handles de-duplication of matches. Performance and behavioral
	// characteristics are determined by the chosen implementation.
	//
	// If nil, a NopCacher is used, which disables de-duplication.
	MatchCacher Cacher

	// DataSourceErrorBehavior determines what happens when one of the data
	// sources encounters an unrecoverable error.
	DataSourceErrorBehavior ErrorBehavior
}

// Execute runs the search, blocking until all data sources have been exhausted.
//
// If DataSourceErrorBehavior is set to ErrorBehaviorContinue, the search will
// continue even if one or more data sources encounter an unrecoverable error.
// If DataSourceErrorBehavior is set to ErrorBehaviorCancel and a data source
// encounters an unrecoverable error, Execute will return the encountered error.
func (s Search) Execute(ctx context.Context) error {
	err := s.ValidateParameters()
	if err != nil {
		return err
	}

	err = ctx.Err()
	if err != nil {
		return err
	}

	// If no Cacher was supplied, disable de-duplication using a NopCacher
	matches := s.MatchCacher
	if matches == nil {
		matches = NopCacher{}
	}

	// For both filter functions, default to matching everything
	derFilter := s.DERFilter
	filter := s.Filter
	if derFilter == nil {
		derFilter = func(_ []byte) bool {
			return true
		}
	}
	if filter == nil {
		filter = func(_ *x509.Certificate) bool {
			return true
		}
	}

	ctx, cancel := context.WithCancelCause(ctx)

	var wg sync.WaitGroup
	certs := make(chan []byte, len(s.DataSources))

	// Allow each data source to send certificates concurrently
	for _, dataSource := range s.DataSources {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := dataSource.Source(ctx, certs)
			if err != nil && s.DataSourceErrorBehavior == ErrorBehaviorCancel {
				fmt.Fprintf(os.Stderr, "data source encountered error: %s\n", err.Error())
				cancel(err)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(certs)
	}()

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case certBytes, ok := <-certs:
			// If the channel is closed, the search has finished
			if !ok {
				return nil
			}

			// If the certificate doesn't match the pre-parse filter function,
			// ignore it
			if !derFilter(certBytes) {
				continue
			}

			// Certificates must be parseable ASN.1 DER data
			cert, err := x509.ParseCertificate(certBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "parsing certificate: %s\n", err.Error())
				continue
			}

			// If the certificate doesn't match the filter function, ignore it
			if !filter(cert) {
				continue
			}

			// Add this match to the cache. If it has been seen before, skip
			// running MatchCallback
			if matches.Cache(cert) {
				continue
			}

			s.MatchCallback(cert)
		}
	}
}

func (s Search) ValidateParameters() error {
	// You must supply either DERFilter or Filter, or both
	if s.DERFilter == nil && s.Filter == nil {
		return errors.New("nil filter functions")
	}

	if s.MatchCallback == nil {
		return errors.New("nil match callback function")
	}

	if len(s.DataSources) == 0 {
		return errors.New("no data sources")
	}

	return nil
}
