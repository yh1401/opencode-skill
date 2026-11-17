package prometheus

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
)

var (
	// 以下值从 Prometheus 代码库复制而来:
	// https://github.com/prometheus/prometheus/blob/main/web/api/v1/api.go#L884-L905

	// minTime 是可选时间范围开始的默认时间戳。
	// 暴露出来让下游项目引用。
	//
	// 历史说明: 这应该是 time.Unix(math.MinInt64/1000, 0).UTC(),
	// 但由于过去的误解，它被设置为更高的值。
	// 这个值对于实际用途来说仍然足够低，所以我们现在不想改变它，
	// 以免给这个变量的导入者造成混淆。
	minTime = time.Unix(math.MinInt64/1000+62135596801, 0).UTC()

	// maxTime 是可选时间范围结束的默认时间戳。
	// 暴露出来让下游项目引用。
	//
	// 历史说明: 这应该是 time.Unix(math.MaxInt64/1000, 0).UTC(),
	// 但由于过去的误解，它被设置为较低的值。
	// 这个值对于实际用途来说仍然足够高，所以我们现在不想改变它，
	// 以免给这个变量的导入者造成混淆。
	maxTime = time.Unix(math.MaxInt64/1000-62135596801, 999999999).UTC()

	minTimeFormatted = minTime.Format(time.RFC3339Nano)
	maxTimeFormatted = maxTime.Format(time.RFC3339Nano)
)

// ParseTimestamp 从字符串解析时间戳。时间戳可以是 Unix 时间戳或 RFC3339 格式。
//
// 从 Prometheus 代码库复制:
// https://github.com/prometheus/prometheus/blob/main/web/api/v1/api.go#L2082-L2103
func ParseTimestamp(s string) (time.Time, error) {
	if t, err := strconv.ParseFloat(s, 64); err == nil {
		s, ns := math.Modf(t)
		ns = math.Round(ns*1000) / 1000
		return time.Unix(int64(s), int64(ns*float64(time.Second))).UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}

	// 标准库的时间解析器只能处理 4 位年份。作为解决方法，我们至少要支持我们自己的边界时间。
	// 上下文: https://github.com/prometheus/client_golang/issues/614
	// 上游问题: https://github.com/golang/go/issues/20555
	switch s {
	case minTimeFormatted:
		return minTime, nil
	case maxTimeFormatted:
		return maxTime, nil
	}
	return time.Time{}, fmt.Errorf("无法将 %q 解析为有效的时间戳", s)
}

// ParseTimestampOrDuration 为 Prometheus 的上游时间戳处理提供扩展的时间戳支持。
// LLM 经常使用持续时间字符串（例如 "1h", "5m" 等）。
// 即使工具参数描述被扩展为更明确地说明支持的格式，
// 我经常观察到 LLM 首先尝试持续时间字符串，失败，观察错误，
// 再尝试 2 或 3 次不同的格式，可能会调用 shell 命令 `date`/`python` 来处理时间等。
// 从一开始就接受持续时间字符串可以避免很多 LLM 混淆和失败/额外的工具调用。
func ParseTimestampOrDuration(s string) (time.Time, error) {
	if t, err := ParseTimestamp(s); err == nil {
		return t, nil
	}

	if dur, err := model.ParseDuration(s); err == nil {
		// 持续时间始终表示相对于现在的过去时间。
		// "5m" 和 "-5m" 都表示 "5 分钟前" -- 我们规范化负持续时间，
		// 以便调用者（通常是 LLM）无论符号约定如何都能获得一致的行为。
		timeDur := time.Duration(dur)
		if timeDur < 0 {
			timeDur = -timeDur
		}
		return time.Now().Add(-timeDur), nil
	}

	return time.Time{}, fmt.Errorf("无法将 %q 解析为有效的时间戳或持续时间", s)
}
