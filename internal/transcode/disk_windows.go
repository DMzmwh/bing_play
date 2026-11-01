//go:build windows

package transcode

import "golang.org/x/sys/windows"

// freeDiskBytes 返回目录所在磁盘的可用字节数，无法获取时返回 0。
func freeDiskBytes(dir string) int64 {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(path, &freeToCaller, &total, &totalFree); err != nil {
		return 0
	}
	return int64(freeToCaller)
}
