# x509search

A library to build custom search tools for X.509 certificates

## Usage

Here's an example of using x509search to scan through a tiled CT log for
precertificates issued by Let's Encrypt:

```go
package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/letsencrypt/x509search"
	"github.com/letsencrypt/x509search/staticctapi"
)

func main() {
	rome2025h1, err := staticctapi.NewLog("https://rome2025h1.fly.storage.tigris.dev/")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	now := time.Now()
	search := x509search.Search{
		// Filter by the Organization Name of the certificate issuer
		Filter: func(cert *x509.Certificate) bool {
			if len(cert.Issuer.Organization) != 1 {
				return false
			}
			return cert.Issuer.Organization[0] == "Let's Encrypt"
		},
		// Print out the issuer and subject info every time there's a match
		MatchCallback: func(cert *x509.Certificate) {
			fmt.Printf("Issuer: %s Subject: %s\n", cert.Issuer.String(), cert.Subject.String())
		},
		// Configure a single data source: the Rome2025h1 tiled log
		DataSources: []x509search.Sourcer{
			staticctapi.DataSource{
				Log:                    rome2025h1,
				IncludePrecertificates: true,
				IncludeCertificates:    false,
				StartTimeInclusive:     now.Add(-3*time.Hour - 1*time.Minute),
				EndTimeInclusive:       now.Add(-3 * time.Hour),
				MaxConnections:         10,
			},
		},
	}

	search.Execute(context.Background())
}
```
