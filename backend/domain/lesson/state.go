package domain

import "fmt"

// AllowedTransition 是 Lesson 生命周期的唯一状态转移规则入口。
// 所有 Worker 和 Service 都必须通过这里校验，避免不同模块各自维护一套状态逻辑。
func AllowedTransition(from, to LessonStatus) bool {
	if from == StatusFailed {
		return to == StatusRecognizing || to == StatusStructuring || to == StatusResearching || to == StatusTeaching || to == StatusSolving || to == StatusPlanning || to == StatusRendering
	}
	if from == StatusAwaitingConfirmation {
		return to == StatusStructuring || to == StatusRejected
	}
	switch from {
	case StatusCreated:
		return to == StatusRecognizing
	case StatusRecognizing:
		return to == StatusAwaitingConfirmation || to == StatusStructuring
	case StatusStructuring:
		return to == StatusSolving || to == StatusResearching || to == StatusAwaitingConfirmation
	case StatusResearching:
		return to == StatusResearchReady || to == StatusResearchUnavailable
	case StatusResearchReady:
		return to == StatusTeaching
	case StatusResearchUnavailable:
		return to == StatusResearching
	case StatusTeaching:
		return to == StatusPlanning || to == StatusFailed
	case StatusSolving:
		return to == StatusPlanning || to == StatusAwaitingConfirmation
	case StatusPlanning:
		return to == StatusRendering
	case StatusRendering:
		return to == StatusReady
	default:
		return false
	}
}

func ValidateTransition(from, to LessonStatus) error {
	if !AllowedTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// Stage 将内部状态映射成前端可展示的阶段名称，避免 API 暴露过多实现细节。
func Stage(status LessonStatus) string {
	switch status {
	case StatusCreated, StatusRecognizing:
		return "recognizing"
	case StatusAwaitingConfirmation:
		return "confirmation"
	case StatusStructuring:
		return "structuring"
	case StatusResearching, StatusResearchReady, StatusResearchUnavailable:
		return "research"
	case StatusTeaching:
		return "teaching"
	case StatusSolving:
		return "solving"
	case StatusPlanning:
		return "planning"
	case StatusRendering:
		return "rendering"
	case StatusReady:
		return "ready"
	case StatusRejected:
		return "rejected"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Progress 是 MVP 阶段的确定性进度映射；确认等待状态单独保留在识别阶段之后。
func Progress(status LessonStatus) int {
	for _, s := range []LessonStatus{StatusCreated, StatusRecognizing, StatusStructuring, StatusResearching, StatusResearchReady, StatusTeaching, StatusSolving, StatusPlanning, StatusRendering, StatusReady} {
		if s == status {
			return map[LessonStatus]int{StatusCreated: 0, StatusRecognizing: 15, StatusStructuring: 35, StatusResearching: 40, StatusResearchReady: 45, StatusTeaching: 60, StatusSolving: 65, StatusPlanning: 75, StatusRendering: 90, StatusReady: 100}[s]
		}
	}
	if status == StatusAwaitingConfirmation {
		return 20
	}
	return 0
}

// NextAction 让前端只依据当前响应就能决定用户下一步操作。
func NextAction(status LessonStatus) string {
	switch status {
	case StatusAwaitingConfirmation:
		return "confirm_question"
	case StatusResearchReady:
		return "generate_tutorial"
	case StatusResearchUnavailable:
		return "retry_research"
	case StatusTeaching:
		return "wait"
	case StatusFailed:
		return "retry"
	case StatusReady:
		return "practice"
	case StatusRejected:
		return "create_lesson"
	default:
		return "wait"
	}
}
