package boulder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	boulderCmd "github.com/letsencrypt/boulder/cmd"
	"github.com/letsencrypt/boulder/db"
	"github.com/letsencrypt/boulder/sa"
)

type Database struct {
	handle *db.WrappedMap
}

func NewDatabase(configFile string) (*Database, error) {
	configBytes, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config *boulderCmd.DBConfig
	err = json.Unmarshal(configBytes, config)
	if err != nil {
		return nil, err
	}

	handle, err := sa.InitWrappedDb(*config, nil, nil)
	if err != nil {
		return nil, err
	}

	return &Database{handle: handle}, nil
}

func (d *Database) GetCertificateIdFromIssuedTime(ctx context.Context, issued time.Time) (int64, error) {
	// TODO: Make this work to find either the first or the last occurrence

	startCert, err := d.SelectCertificate(ctx, "ORDER BY id ASC")
	if err != nil {
		return -1, fmt.Errorf("selecting oldest issued certificate: %w", err)
	}

	endCert, err := d.SelectCertificate(ctx, "ORDER BY id DESC")
	if err != nil {
		return -1, fmt.Errorf("selecting newest issued certificate: %w", err)
	}

	start := startCert.ID
	end := endCert.ID

	// TODO: Implement binary search

	return -1, errors.New("not implemented")
}

const certFields = "id, registrationID, serial, digest, der, issued, expires"

func (d *Database) SelectCertificate(ctx context.Context, q string, args ...interface{}) (sa.CertWithID, error) {
	var cert sa.CertWithID
	err := d.handle.SelectOne(
		ctx,
		&cert,
		"SELECT "+certFields+" FROM certificates "+q+" LIMIT 1",
		args...,
	)
	return cert, err
}

func (d *Database) SelectCertificatesByIdRange(ctx context.Context, startId int64, endId int64) ([]sa.CertWithID, error) {
	certs, err := sa.SelectCertificates(
		ctx,
		d.handle,
		"WHERE id >= :startId AND id <= :endId",
		map[string]interface{}{
			"startId": startId,
			"endId":   endId,
		},
	)
	if err != nil {
		return nil, err
	}

	return certs, nil
}
