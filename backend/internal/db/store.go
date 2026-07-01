package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(driver, path string) (*Store, error) {
	if strings.TrimSpace(driver) == "" {
		driver = "sqlite"
	}
	if driver != "sqlite" {
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	if strings.TrimSpace(path) == "" {
		path = ":memory:"
	}
	dsn := path
	switch {
	case dsn == ":memory:":
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	case strings.HasPrefix(dsn, "file:"):
		dsn = appendSQLitePragmas(dsn, []string{
			"_pragma=foreign_keys(1)",
			"_pragma=busy_timeout(5000)",
			"_pragma=journal_mode(WAL)",
		})
	default:
		dsn = appendSQLitePragmas(dsn, []string{
			"_pragma=foreign_keys(1)",
			"_pragma=busy_timeout(5000)",
			"_pragma=journal_mode(WAL)",
		})
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func appendSQLitePragmas(dsn string, pragmas []string) string {
	if len(pragmas) == 0 {
		return dsn
	}
	existing := map[string]struct{}{}
	if queryStart := strings.Index(dsn, "?"); queryStart >= 0 {
		values, err := url.ParseQuery(dsn[queryStart+1:])
		if err == nil {
			for key, rawValues := range values {
				if !strings.EqualFold(strings.TrimSpace(key), "_pragma") {
					existing[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
					continue
				}
				for _, value := range rawValues {
					existing[pragmaName(value)] = struct{}{}
				}
			}
		}
	}
	missing := make([]string, 0, len(pragmas))
	for _, pragma := range pragmas {
		key := pragmaName(strings.TrimPrefix(pragma, "_pragma="))
		if _, ok := existing[key]; ok {
			continue
		}
		missing = append(missing, pragma)
	}
	if len(missing) == 0 {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + strings.Join(missing, "&")
}

func pragmaName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	name, _, _ := strings.Cut(raw, "(")
	name, _, _ = strings.Cut(name, "=")
	return strings.TrimSpace(name)
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func (s *Store) NowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
