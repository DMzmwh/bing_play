package transcode

import "runtime"

// Network 表示文件来源的网络状况。
//
// 本地文件固定为 NetworkLocal；远程/种子流式来源可用于决定是否推迟转换。
type Network string

const (
	NetworkLocal   Network = "local"
	NetworkLAN     Network = "lan"
	NetworkWAN     Network = "wan"
	NetworkMetered Network = "metered"
	NetworkOffline Network = "offline"
)

// Action 转换调度结果。
type Action string

const (
	// ActionPlayDirect 原文件可直接播放，无需任何处理。
	ActionPlayDirect Action = "play_direct"
	// ActionPlayConverted 播放转换后的缓存文件。
	ActionPlayConverted Action = "play_converted"
	// ActionConvert 需要异步转换。
	ActionConvert Action = "convert"
	// ActionDefer 当前条件不适合转换，推迟到条件满足时再执行。
	ActionDefer Action = "defer"
	// ActionFallback 无法转换，回退到原始文件播放并提示。
	ActionFallback Action = "fallback"
)

// Policy 转换策略。零值字段由 DefaultPolicy 填充。
type Policy struct {
	// MinFreeDiskBytes 转换时至少保留的磁盘余量。
	MinFreeDiskBytes int64
	// OutputSizeFactor 预估输出体积相对源文件的倍率。
	OutputSizeFactor float64
	// DeferOnMeteredNetwork 计量网络下推迟大文件转换。
	DeferOnMeteredNetwork bool
	// PreferHardware 优先尝试硬件编码，失败自动回退软件编码。
	PreferHardware bool
	// HardwareAboveBytes 超过该体积的文件即使未开启 PreferHardware 也优先硬件编码。
	HardwareAboveBytes int64
	// AlwaysConvert 忽略兼容性判断，强制转换。
	AlwaysConvert bool
}

// DefaultPolicy 返回适合本地播放的默认策略。
func DefaultPolicy() Policy {
	return Policy{
		MinFreeDiskBytes:      1 << 30,
		OutputSizeFactor:      1.2,
		DeferOnMeteredNetwork: true,
		PreferHardware:        true,
		HardwareAboveBytes:    200 << 20,
	}
}

// Conditions 决策所依赖的运行时条件。
type Conditions struct {
	FFmpegAvailable bool
	FreeDiskBytes   int64
	Network         Network
	HardwareEncoder bool
	SizeBytes       int64
	Target          Target
}

// Decision 转换调度决策。
type Decision struct {
	Action               Action        `json:"action"`
	Reason               Reason        `json:"reason"`
	Compatibility        Compatibility `json:"compatibility"`
	UseHardware          bool          `json:"useHardware"`
	EstimatedOutputBytes int64         `json:"estimatedOutputBytes"`
}

// Decide 根据文件格式、目标平台能力、磁盘、网络与设备条件决定处理方式。
//
// 判断顺序：
//  1. 目标内核能直接播放 → 不转换；
//  2. 缺少 ffmpeg → 回退原文件播放；
//  3. 计量网络且开启推迟策略 → defer；
//  4. 磁盘余量不足 → 回退原文件播放；
//  5. 其余情况异步转换。
func Decide(f Format, cond Conditions, policy Policy) Decision {
	if policy.OutputSizeFactor <= 0 {
		policy.OutputSizeFactor = DefaultPolicy().OutputSizeFactor
	}
	if cond.Target == "" {
		cond.Target = TargetForOS(runtime.GOOS)
	}

	decision := Decision{Compatibility: Evaluate(f, cond.Target)}

	if decision.Compatibility.Playable && !policy.AlwaysConvert {
		decision.Action = ActionPlayDirect
		decision.Reason = ReasonCompatible
		return decision
	}

	if !cond.FFmpegAvailable {
		decision.Action = ActionFallback
		decision.Reason = ReasonFFmpegMissing
		return decision
	}

	if policy.DeferOnMeteredNetwork && cond.Network == NetworkMetered {
		decision.Action = ActionDefer
		decision.Reason = ReasonMeteredNetwork
		return decision
	}

	size := f.SizeBytes
	if cond.SizeBytes > 0 {
		size = cond.SizeBytes
	}
	estimated := int64(float64(size) * policy.OutputSizeFactor)
	decision.EstimatedOutputBytes = estimated

	if cond.FreeDiskBytes > 0 && cond.FreeDiskBytes < estimated+policy.MinFreeDiskBytes {
		decision.Action = ActionFallback
		decision.Reason = ReasonInsufficientDisk
		return decision
	}

	decision.Action = ActionConvert
	decision.Reason = ReasonVideoUnsupported
	if len(decision.Compatibility.Reasons) > 0 {
		decision.Reason = decision.Compatibility.Reasons[0]
	}
	decision.UseHardware = cond.HardwareEncoder &&
		(policy.PreferHardware || (policy.HardwareAboveBytes > 0 && size >= policy.HardwareAboveBytes))
	return decision
}
