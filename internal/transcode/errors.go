package transcode

import "errors"

// 转码相关错误。错误文本同时作为 i18n 消息 ID，调用方直接 T(err.Error()) 取文案。
var (
	// ErrFFmpegMissing 随包与系统都找不到 ffmpeg。
	ErrFFmpegMissing = errors.New("transcode.error.ffmpeg_missing")
	// ErrInsufficientDisk 磁盘余量不足以完成转换。
	ErrInsufficientDisk = errors.New("transcode.error.insufficient_disk")
	// ErrDeferredNetwork 计量网络下暂缓转换。
	ErrDeferredNetwork = errors.New("transcode.error.metered_network")
	// ErrConvertFailed 转换过程失败，已回退原文件播放。
	ErrConvertFailed = errors.New("transcode.error.convert_failed")
	// ErrCanceled 任务被取消。
	ErrCanceled = errors.New("transcode.error.canceled")
	// ErrSchedulerClosed 调度器已关闭。
	ErrSchedulerClosed = errors.New("transcode.error.closed")
)

func errForReason(reason Reason) error {
	switch reason {
	case ReasonFFmpegMissing:
		return ErrFFmpegMissing
	case ReasonInsufficientDisk:
		return ErrInsufficientDisk
	case ReasonMeteredNetwork:
		return ErrDeferredNetwork
	default:
		return nil
	}
}
