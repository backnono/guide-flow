package lesson

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type Service struct {
	repo        LessonRepository
	queue       JobQueue
	images      ImageStore
	now         func() time.Time
	mu          sync.Mutex
	idempotency map[string]string
}

func NewService(repo LessonRepository, queue JobQueue, images ImageStore) *Service {
	return &Service{repo: repo, queue: queue, images: images, now: domain.Now, idempotency: map[string]string{}}
}

func (s *Service) CreateLesson(ctx context.Context, image io.Reader, imageName string, profile domain.LearnerProfile) (*domain.Lesson, error) {
	return s.CreateLessonWithIdempotency(ctx, image, imageName, profile, "")
}

func (s *Service) CreateLessonWithIdempotency(ctx context.Context, image io.Reader, imageName string, profile domain.LearnerProfile, key string) (*domain.Lesson, error) {
	if key != "" {
		if durable, ok := s.repo.(IdempotencyRepository); ok {
			if existing, err := durable.GetByIdempotencyKey(ctx, key); err == nil {
				return existing, nil
			} else if !errors.Is(err, domain.ErrNotFound) {
				return nil, err
			}
		}
		// 在创建期间持有锁，保证同一个幂等键不会并发创建出两个 Lesson。
		s.mu.Lock()
		defer s.mu.Unlock()
		if id := s.idempotency[key]; id != "" {
			return s.repo.Get(ctx, id)
		}
	}
	id := fmt.Sprintf("lesson-%d", s.now().UnixNano())
	now := s.now()
	lesson := &domain.Lesson{ID: id, Status: domain.StatusCreated, LearnerProfile: profile, ImageName: imageName, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now}
	lesson.ResearchStatus = domain.ResearchNotStarted
	lesson.TutorialStatus = domain.TutorialNotStarted
	if err := s.images.Save(ctx, id, image); err != nil {
		return nil, err
	}
	if refs, ok := s.images.(ImageReferenceStore); ok {
		imageRefs, refErr := refs.References(ctx, id)
		if refErr != nil {
			_ = s.images.Delete(ctx, id)
			return nil, refErr
		}
		lesson.ImageObjectKey = imageRefs.OriginalObjectKey
		lesson.ThumbnailObjectKey = imageRefs.ThumbnailObjectKey
	}
	if err := s.repo.Create(ctx, lesson); err != nil {
		// 对象存储和数据库不是同一个事务；数据库写入失败时主动删除孤儿图片，避免长期积累无主对象。
		_ = s.images.Delete(ctx, id)
		return nil, err
	}
	if _, durableOutbox := s.repo.(OutboxRepository); !durableOutbox {
		if err := s.queue.Enqueue(id); err != nil {
			// 内存 Demo 没有持久化 outbox，只能在当前进程内直接投递。
			_ = s.images.Delete(ctx, id)
			return nil, err
		}
	}
	if key != "" {
		s.idempotency[key] = id
	}
	return lesson, nil
}

func (s *Service) GetLesson(ctx context.Context, id string) (*domain.Lesson, error) {
	return s.repo.Get(ctx, id)
}

// OpenLessonImage 只允许按 Lesson ID 读取原题图片，先查 Lesson 再打开对象，
// 避免把对象存储 key 暴露给前端，也避免通过任意 key 读取其他图片。
func (s *Service) OpenLessonImage(ctx context.Context, id string) (io.ReadCloser, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.images.Open(ctx, id)
}

func (s *Service) ConfirmQuestion(ctx context.Context, id, text string) (*domain.Lesson, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if lesson.Status != domain.StatusAwaitingConfirmation {
		return nil, domain.ErrInvalidTransition
	}
	if lesson.QuestionDraft == nil {
		return nil, domain.ErrNeedsConfirmation
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, domain.ErrQuestionInvalid
	}
	if len([]rune(text)) > 2000 {
		return nil, domain.ErrQuestionTooLong
	}
	originalQuestionText := lesson.QuestionDraft.Text
	// 用户确认会产生新版本，不覆盖 OCR 版本，便于追溯识别结果和用户修订内容。
	lesson.QuestionDraft.Text = strings.TrimSpace(text)
	// 只有用户实际改过题面时才丢弃视觉结构；纯确认要保留图形关系，避免再次退化成规则文本解析。
	if lesson.QuestionDraft.Text != originalQuestionText {
		// 修改题干会使原图结构、题目分析、检索结果和解法全部失效，必须整体清空，避免新旧题面混合求解。
		lesson.Question = nil
		lesson.QuestionAnalysis = nil
		lesson.ResearchContext = nil
		lesson.SolutionCandidates = nil
		lesson.SolutionDraft = nil
		lesson.SolutionTrace = nil
		lesson.VerifiedSolution = nil
		lesson.LessonPlan = nil
		lesson.ResearchStatus = domain.ResearchNotStarted
		lesson.TutorialStatus = domain.TutorialNotStarted
		lesson.QuestionVersionID = 0
	}
	lesson.QuestionDraft.Confidence = 1
	lesson.QuestionVersions = append(lesson.QuestionVersions, domain.QuestionVersion{ID: int64(len(lesson.QuestionVersions) + 1), Text: lesson.QuestionDraft.Text, Source: "user", Confidence: 1, CreatedAt: s.now()})
	lesson.CurrentQuestionVersion = int64(len(lesson.QuestionVersions))
	if err := s.transition(ctx, lesson, domain.StatusStructuring); err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(id); err != nil {
		return nil, err
	}
	return lesson, nil
}

// RequestTutorial 是检索与教程之间的显式用户动作；参考解答已落库时无需重新检索。
func (s *Service) RequestTutorial(ctx context.Context, id string) (*domain.Lesson, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if lesson.Status != domain.StatusResearchReady || lesson.ResearchContext == nil {
		return nil, domain.ErrInvalidTransition
	}
	if lesson.ResearchStatus != domain.ResearchFound && lesson.ResearchStatus != domain.ResearchVerified {
		return nil, domain.ErrInvalidTransition
	}
	lesson.TutorialStatus = domain.TutorialGenerating
	if err := s.transition(ctx, lesson, domain.StatusTeaching); err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(id); err != nil {
		return nil, err
	}
	return lesson, nil
}

// RetryTutorial 只允许从教程失败阶段恢复，保证不会因为讲解失败重复消耗搜索配额。
func (s *Service) RetryTutorial(ctx context.Context, id string) (*domain.Lesson, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if lesson.Status != domain.StatusFailed || lesson.FailedFrom != domain.StatusTeaching || lesson.ResearchContext == nil {
		return nil, domain.ErrNotRetryable
	}
	lesson.TutorialStatus = domain.TutorialGenerating
	lesson.RetryCount++
	if err = s.transition(ctx, lesson, domain.StatusTeaching); err != nil {
		return nil, err
	}
	if err = s.queue.Enqueue(id); err != nil {
		return nil, err
	}
	return lesson, nil
}

func (s *Service) RejectQuestion(ctx context.Context, id string) (*domain.Lesson, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = s.transition(ctx, lesson, domain.StatusRejected); err != nil {
		return nil, err
	}
	return lesson, nil
}

func (s *Service) RetryLesson(ctx context.Context, id string) (*domain.Lesson, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if lesson.Status == domain.StatusResearchUnavailable {
		lesson.ResearchStatus = domain.ResearchSearching
		if err = s.transition(ctx, lesson, domain.StatusResearching); err != nil {
			return nil, err
		}
		if err = s.queue.Enqueue(id); err != nil {
			return nil, err
		}
		return lesson, nil
	}
	if lesson.Status != domain.StatusFailed || lesson.FailedFrom == "" {
		return nil, domain.ErrNotRetryable
	}
	retryFrom := lesson.FailedFrom
	if retryFrom == domain.StatusSolving && lesson.Question != nil && strings.TrimSpace(lesson.Question.Topic) == "" {
		// 视觉结构化结果不完整时必须重新读图，不能从 solving 重复消费空 topic 的旧快照。
		retryFrom = domain.StatusRecognizing
		lesson.Question = nil
	}
	if retryFrom == domain.StatusSolving && strings.Contains(lesson.Error, "llm draft must include") {
		// 旧版本可能已把错误的 Demo 结构化结果落库；先回退到结构化阶段，避免重试继续消费脏题型。
		retryFrom = domain.StatusStructuring
	}
	if retryFrom == domain.StatusTeaching {
		lesson.TutorialStatus = domain.TutorialGenerating
	}
	// 从可安全恢复的阶段继续，避免重复 OCR 或重新创建 Lesson。
	lesson.RetryCount++
	if err = s.transition(ctx, lesson, retryFrom); err != nil {
		return nil, err
	}
	if err = s.queue.Enqueue(id); err != nil {
		return nil, err
	}
	return lesson, nil
}

func (s *Service) SubmitPractice(ctx context.Context, id, answer string) (domain.PracticeResult, error) {
	lesson, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.PracticeResult{}, err
	}
	if lesson.Status != domain.StatusReady || lesson.LessonPlan == nil {
		return domain.PracticeResult{}, domain.ErrNotReady
	}
	expected := lesson.LessonPlan.Practice.Answer
	// 先做安全的数值等值判断，再回退到字符串比较，避免“12”和“12 页”被误判为不同答案。
	correct := answersEqual(answer, expected)
	feedback := "再想想：把题目中的数量平均分成对应的份数。"
	if correct {
		feedback = "答对啦！"
	}
	result := domain.PracticeResult{Attempt: len(lesson.PracticeResults) + 1, Answer: answer, Correct: correct, Feedback: feedback, ExpectedAnswer: expected}
	lesson.PracticeResults = append(lesson.PracticeResults, result)
	lesson.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, lesson); err != nil {
		return domain.PracticeResult{}, err
	}
	return result, nil
}

func (s *Service) transition(ctx context.Context, l *domain.Lesson, to domain.LessonStatus) error {
	if err := domain.ValidateTransition(l.Status, to); err != nil {
		return err
	}
	from := l.Status
	l.Status = to
	l.UpdatedAt = s.now()
	// 状态事件与当前状态在同一次 Repository 更新中写入，保证后续可以解释任务经过了哪些阶段。
	l.StatusEvents = append(l.StatusEvents, domain.StatusEvent{From: from, To: to, Stage: domain.Stage(to), CreatedAt: l.UpdatedAt})
	return s.repo.Update(ctx, l)
}

var numberPattern = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)

func answersEqual(got, expected string) bool {
	got, expected = strings.TrimSpace(got), strings.TrimSpace(expected)
	if got == expected {
		return true
	}
	// 当前 MVP 只抽取简单整数/小数；无法安全解析时不猜测，直接判为错误。
	gm, em := numberPattern.FindString(got), numberPattern.FindString(expected)
	if gm == "" || em == "" {
		return false
	}
	g, ge := strconv.ParseFloat(gm, 64)
	e, ee := strconv.ParseFloat(em, 64)
	return ge == nil && ee == nil && g == e
}
