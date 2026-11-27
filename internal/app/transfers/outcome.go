package transfers

import (
	"errors"

	filefs "yaare/internal/adapters/files"
)

type Outcome struct {
	State, Code, Message string
	CanResume            bool
}

func ResolveOutcome(intent string, cancelled, timedOut bool, runErr, cleanupErr error, describeError func(error) (string, string)) Outcome {
	switch {
	case intent == "cancel":
		message := "任务已取消。"
		if cleanupErr != nil {
			message = "任务已取消；连接不可用，未提交的临时文件暂未清理。"
		}
		return Outcome{State: "cancelled", Code: "CANCELLED", Message: message}
	case intent == "pause":
		return Outcome{State: "paused", Message: "已暂停，继续时将校验已传内容。", CanResume: true}
	case errors.Is(runErr, filefs.ErrConflict):
		return Outcome{State: "conflict", Message: "目标已存在，请选择处理方式。", CanResume: true}
	case cancelled && runErr == nil:
		return Outcome{State: "cancelled", Code: "CANCELLED", Message: "任务已取消。"}
	case timedOut:
		return Outcome{State: "failed", Code: "FILE_TIMEOUT", Message: "传输长时间无响应，请检查连接后继续。", CanResume: true}
	case runErr != nil:
		code, message := describeError(runErr)
		return Outcome{State: "failed", Code: code, Message: message, CanResume: code != "FILE_RESUME_CHANGED"}
	default:
		return Outcome{State: "completed", Message: "传输已完成。"}
	}
}
