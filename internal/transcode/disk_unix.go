//go:build !windows

package transcode

import "golang.org/x/sys/unix"

// freeDiskBytes 返回目录所在磁盘的可用字节数，无法获取时返回 0。
func freeDiskBytes(dir string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}
