package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSQLiteDSNTimeFormat 校验按 InitDB 使用的 DSN 打开连接时，
// time.Time 会按 SQLite 标准格式写入，而不是带单调时钟后缀的 String() 结果。
func TestSQLiteDSNTimeFormat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "time.db")
	sqlDB, err := sql.Open("sqlite", dbPath+sqliteDSNParams)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if _, err := sqlDB.Exec(`CREATE TABLE time_probe (ts DATETIME)`); err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}

	now := time.Now()
	if _, err := sqlDB.Exec(`INSERT INTO time_probe (ts) VALUES (?)`, now); err != nil {
		t.Fatalf("写入时间失败: %v", err)
	}

	var raw string
	if err := sqlDB.QueryRow(`SELECT quote(ts) FROM time_probe`).Scan(&raw); err != nil {
		t.Fatalf("读取时间失败: %v", err)
	}

	want := "'" + now.Format("2006-01-02 15:04:05") + "'"
	if raw != want {
		t.Fatalf("写入时间 = %s, want %s", raw, want)
	}
	if strings.Contains(raw, "m=+") {
		t.Fatalf("时间包含单调时钟后缀: %s", raw)
	}

	var got time.Time
	if err := sqlDB.QueryRow(`SELECT ts FROM time_probe`).Scan(&got); err != nil {
		t.Fatalf("按 time.Time 读取失败: %v", err)
	}
	if !got.Equal(now.Truncate(time.Second)) {
		t.Fatalf("读取时间 = %v, want %v", got, now.Truncate(time.Second))
	}
}
