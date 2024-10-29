package x509search

import (
	"crypto/sha256"
	"crypto/x509"

	"github.com/bits-and-blooms/bloom/v3"
)

type Cacher interface {
	// Cache adds the given certificate to the cache and returns whether it was
	// already present.
	Cache(*x509.Certificate) bool
}

// NopCacher does not cache certificates.
type NopCacher struct{}

// Cache always returns false.
func (c NopCacher) Cache(_ *x509.Certificate) bool {
	return false
}

// BloomCacher uses a bloom filter to cache certificate matches. Because bloom
// filters are probabilistic data structures, they may occasionally report
// false-positives, resulting in certificate matches being ignored by the search
// algorithm. If full-and-complete results are required with absolute certainty,
// do not use BloomCacher.
type BloomCacher struct {
	filter *bloom.BloomFilter
}

// NewBloomCacher returns a BloomCacher that uses countEstimate and
// falsePositiveRate to determine the size of the underlying bloom filter.
func NewBloomCacher(countEstimate uint, falsePositiveRate float64) *BloomCacher {
	return &BloomCacher{
		filter: bloom.NewWithEstimates(countEstimate, falsePositiveRate),
	}
}

// Cache uses a bloom filter to determine membership in the cache.
func (c *BloomCacher) Cache(cert *x509.Certificate) bool {
	return c.filter.TestOrAdd(cert.Raw)
}

// Sha256MapCacher uses a map of SHA-256 certificate fingerprints to cache
// certificates.
type Sha256MapCacher struct {
	certs map[[32]byte]bool
}

func NewSha256MapCacher() *Sha256MapCacher {
	return &Sha256MapCacher{
		certs: make(map[[32]byte]bool),
	}
}

// Cache calculates the SHA-256 fingerprint of the given certificate and uses it
// to determine membership in the cache.
func (c *Sha256MapCacher) Cache(cert *x509.Certificate) bool {
	// Use the certificate's Sha256 fingerprint as the map key
	hash := sha256.Sum256(cert.Raw)

	// When a map key isn't present, Go returns the zero value, so false
	present := c.certs[hash]

	// Cache this certificate in the map and return whether it was present
	c.certs[hash] = true
	return present
}
