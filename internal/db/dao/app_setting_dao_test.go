package dao

import (
	"fmt"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// newTestAppSettingDAO 创建一个内存 SQLite 的 AppSettingDAO。
func newTestAppSettingDAO(t *testing.T) *AppSettingDAO {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	err = db.Exec(`
CREATE TABLE app_settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`).Error
	if err != nil {
		t.Fatalf("创建 app_settings 表失败: %v", err)
	}

	return NewAppSettingDAO(db)
}

func TestAppSettingGetMissing(t *testing.T) {
	d := newTestAppSettingDAO(t)

	value, err := d.Get("not-exists")
	if err != nil {
		t.Fatalf("查询不存在的设置项失败: %v", err)
	}
	if value != "" {
		t.Fatalf("不存在的设置项返回值 = %q, want empty", value)
	}
}

func TestAppSettingSetUpsert(t *testing.T) {
	d := newTestAppSettingDAO(t)

	if err := d.Set("language", "en"); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := d.Set("language", "zh-CN"); err != nil {
		t.Fatalf("更新写入失败: %v", err)
	}

	value, err := d.Get("language")
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if value != "zh-CN" {
		t.Fatalf("language = %q, want zh-CN", value)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("批量读取失败: %v", err)
	}
	if len(all) != 1 || all["language"] != "zh-CN" {
		t.Fatalf("批量读取结果 = %v", all)
	}
}

func TestAppSettingSetMany(t *testing.T) {
	d := newTestAppSettingDAO(t)

	values := map[string]string{
		"language":              "zh-TW",
		"ui.resource_page_size": "200",
		"ui.accent_color":       "#22c55e",
	}
	if err := d.SetMany(values); err != nil {
		t.Fatalf("批量写入失败: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("批量读取失败: %v", err)
	}
	if len(all) != len(values) {
		t.Fatalf("批量读取行数 = %d, want %d", len(all), len(values))
	}
	for key, want := range values {
		if got := all[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestAppSettingSetManyEmpty(t *testing.T) {
	d := newTestAppSettingDAO(t)

	if err := d.SetMany(nil); err != nil {
		t.Fatalf("空批量写入不应报错: %v", err)
	}
}
