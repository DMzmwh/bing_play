package assets

import (
	"embed"
	"io/fs"
)

// FS 内置资源文件系统（语言包、数据库迁移）。
//
// 使用独立类型而不是直接用 embed.FS，避免与前端资源在 Fx 容器中产生同类型冲突。
type FS embed.FS

// LocalesDir 内置语言包目录。
const LocalesDir = "resources/locales"

// MigrationsDir 内置数据库迁移目录。
const MigrationsDir = "resources/migrations"

func (f FS) Open(name string) (fs.File, error) {
	return embed.FS(f).Open(name)
}

func (f FS) ReadDir(name string) ([]fs.DirEntry, error) {
	return embed.FS(f).ReadDir(name)
}

func (f FS) ReadFile(name string) ([]byte, error) {
	return embed.FS(f).ReadFile(name)
}
