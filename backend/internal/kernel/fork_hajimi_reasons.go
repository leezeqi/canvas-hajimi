package kernel

// 积分不足的稳定原因码（fork 新增）：前端据此提供“从 hajimi 余额转入”，不解析 msg。
// HTTP 状态和业务 code 保持 400，与改动前一致。
const ReasonInsufficientCredits ErrorReason = "insufficient_credits"

func InsufficientCredits(message string) *AppError {
	return &AppError{Status: CodeInvalidArgument, Code: CodeInvalidArgument, Reason: ReasonInsufficientCredits, Message: message}
}
