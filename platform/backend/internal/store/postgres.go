package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// pgMirror 是文件存储的备份镜像：所有写入 best-effort 同步到 PG，
// 文件缺失时从 PG 回读兜底。PG 不可用不影响平台主流程。
type pgMirror struct {
	dsn string

	mu          sync.Mutex
	db          *sql.DB
	ready       bool
	lastAttempt time.Time
	lastErr     string
}

const pgConnTimeout = 3 * time.Second

func newPGMirror(dsn string) (*pgMirror, error) {
	m := &pgMirror{dsn: dsn}
	if err := m.connect(); err != nil {
		return m, err
	}
	return m, nil
}

func (m *pgMirror) connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready && m.db != nil {
		return nil
	}
	if time.Since(m.lastAttempt) < 15*time.Second && m.lastErr != "" {
		return fmt.Errorf("%s", m.lastErr)
	}
	m.lastAttempt = time.Now()

	db, err := sql.Open("postgres", m.dsn)
	if err != nil {
		m.lastErr = err.Error()
		return err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		m.lastErr = err.Error()
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS platform_docs (
		doc_type   text        NOT NULL,
		doc_id     text        NOT NULL,
		payload    jsonb       NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (doc_type, doc_id)
	)`); err != nil {
		_ = db.Close()
		m.lastErr = err.Error()
		return err
	}
	m.db = db
	m.ready = true
	m.lastErr = ""
	return nil
}

func (m *pgMirror) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready {
		return "connected"
	}
	if m.lastErr != "" {
		return "unavailable: " + m.lastErr
	}
	return "idle"
}

func (m *pgMirror) Upsert(docType, docID string, payload []byte) error {
	if err := m.connect(); err != nil {
		return err
	}
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	_, err := db.Exec(`INSERT INTO platform_docs (doc_type, doc_id, payload, updated_at)
		VALUES ($1, $2, $3::jsonb, now())
		ON CONFLICT (doc_type, doc_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		docType, docID, string(payload))
	return err
}

func (m *pgMirror) Get(docType, docID string) ([]byte, bool, error) {
	if err := m.connect(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	var payload []byte
	err := db.QueryRow(`SELECT payload FROM platform_docs WHERE doc_type=$1 AND doc_id=$2`, docType, docID).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return payload, true, nil
}

func (m *pgMirror) List(docType string) ([][]byte, error) {
	if err := m.connect(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	rows, err := db.Query(`SELECT payload FROM platform_docs WHERE doc_type=$1 ORDER BY doc_id`, docType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]byte{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		out = append(out, payload)
	}
	return out, rows.Err()
}

func (m *pgMirror) Delete(docType, docID string) error {
	if err := m.connect(); err != nil {
		return err
	}
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	_, err := db.Exec(`DELETE FROM platform_docs WHERE doc_type=$1 AND doc_id=$2`, docType, docID)
	return err
}
