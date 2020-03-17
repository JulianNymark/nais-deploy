package database

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	log "github.com/sirupsen/logrus"
)

var (
	ErrNotFound = fmt.Errorf("api key not found")
)

const (
	NotFoundMessage = "The specified key does not exist."
)

type Database interface {
	Migrate() error
	Read(team string) ([]ApiKey, error)
	ReadByGroupClaim(group string) ([]ApiKey, error)
	Write(team, groupId string, key []byte) error
	IsErrNotFound(err error) bool
}

type database struct {
	conn *pgx.Conn
}

type ApiKey struct {
	Team    string    `json:"team"`
	GroupId string    `json:"groupId"`
	Key     string    `json:"key"`
	Expires time.Time `json:"expires"`
	Created time.Time `json:"created"`
}

var _ Database = &database{}

func New(dsn string) (Database, error) {
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}

	return &database{
		conn: conn,
	}, nil
}

func (db *database) Migrate() error {
	ctx := context.Background()
	var version int

	query := `SELECT MAX(version) FROM migrations`
	row := db.conn.QueryRow(ctx, query)
	err := row.Scan(&version)

	if err != nil {
		// error might be due to no schema.
		// no way to detect this, so log error and continue with migrations.
		log.Warnf("unable to get current migration version: %s", err)
	}

	for version < len(migrations) {
		log.Infof("migrating database schema to version %d", version+1)

		_, err = db.conn.Exec(ctx, migrations[version])
		if err != nil {
			return fmt.Errorf("migrating to version %d: %s", version+1, err)
		}

		version++
	}

	return nil
}

func (db *database) ReadByGroupClaim(group string) ([]ApiKey, error) {
	ctx := context.Background()
	apiKeys := []ApiKey{}

	query := `SELECT key, team, team_azure_id, created, expires FROM apikey WHERE team_azure_id = $1 AND expires > NOW();`
	rows, err := db.conn.Query(ctx, query, group)

	if err != nil {
		return nil, err
	}

	for rows.Next() {
		apiKey := ApiKey{}
		err := rows.Scan(&apiKey.Key, &apiKey.Team, &apiKey.GroupId, &apiKey.Created, &apiKey.Expires)
		if err != nil {
			return nil, err
		}
		apiKeys = append(apiKeys, apiKey)
	}
	return apiKeys, nil
}

func (db *database) Read(team string) ([]ApiKey, error) {
	ctx := context.Background()
	apiKeys := []ApiKey{}

	query := `SELECT key, team, team_azure_id, created, expires FROM apikey WHERE team = $1 AND expires > NOW();`
	rows, err := db.conn.Query(ctx, query, team)

	if err != nil {
		return nil, err
	}

	for rows.Next() {
		apiKey := ApiKey{}
		err := rows.Scan(&apiKey.Key, &apiKey.Team, &apiKey.GroupId, &apiKey.Created, &apiKey.Expires)
		if err != nil {
			return nil, err
		}
		apiKeys = append(apiKeys, apiKey)
	}
	if len(apiKeys) == 0 {
		return nil, ErrNotFound
	}
	return apiKeys, nil
}

func (db *database) Write(team, groupId string, key []byte) error {
	var query string

	ctx := context.Background()

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("unable to start transaction: %s", err)
	}

	query = `UPDATE apikey SET expires = NOW() WHERE expires > NOW() AND team = $1 AND team_azure_id=$2;`
	_, err = tx.Exec(ctx, query, team, groupId)
	if err != nil {
		return err
	}

	query = `
INSERT INTO apikey (key, team, team_azure_id, created, expires)
VALUES ($1, $2, $3, NOW(), NOW()+MAKE_INTERVAL(years := 5));
`
	_, err = tx.Exec(ctx, query, hex.EncodeToString(key), team, groupId)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (db *database) IsErrNotFound(err error) bool {
	return err == ErrNotFound
}
