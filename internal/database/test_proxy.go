package database

import (
	"context"
	"database/sql"
	"fmt"
)

// DeleteTestProxyPool removes only an unbound pool in a transaction; active project bindings are preserved.
func (db *DB) DeleteTestProxyPool(ctx context.Context, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM test_proxy_bindings WHERE pool_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("pool is bound to a project")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM test_proxy_pools WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// InitTestProxyStorage creates private pool and project-binding storage. Errors stop initialization.
func (db *DB) InitTestProxyStorage() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS test_proxy_pools (id TEXT PRIMARY KEY, document BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS test_proxy_bindings (project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE, pool_id TEXT NOT NULL REFERENCES test_proxy_pools(id));`)
	return err
}

// SaveTestProxyPool atomically replaces a validated, credential-protected document.
func (db *DB) SaveTestProxyPool(ctx context.Context, id string, document []byte) error {
	_, err := db.ExecContext(ctx, `INSERT INTO test_proxy_pools(id,document) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document`, id, document)
	return err
}

// TestProxyPools returns stored documents; credentials must be decrypted only by the service.
func (db *DB) TestProxyPools(ctx context.Context) ([][]byte, error) {
	rows, err := db.QueryContext(ctx, `SELECT document FROM test_proxy_pools ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, rows.Err()
}

// BindTestProxy assigns a pool or removes the binding when poolID is empty. Foreign keys validate references.
func (db *DB) BindTestProxy(ctx context.Context, projectID, poolID string) error {
	if poolID == "" {
		_, err := db.ExecContext(ctx, `DELETE FROM test_proxy_bindings WHERE project_id=?`, projectID)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO test_proxy_bindings(project_id,pool_id) VALUES(?,?) ON CONFLICT(project_id) DO UPDATE SET pool_id=excluded.pool_id`, projectID, poolID)
	return err
}

// TestProxyBinding returns the pool for a project; an absent binding returns an empty string.
func (db *DB) TestProxyBinding(ctx context.Context, projectID string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT pool_id FROM test_proxy_bindings WHERE project_id=?`, projectID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
