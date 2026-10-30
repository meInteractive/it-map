package parser

import "context"

// AIStageDispatcher обрабатывает одну уже полученную lease. Владелец задаётся
// сервером; payload задания не является источником полномочий. Этот тип не
// запускает очередь, таймеры или фоновые операции.
type AIStageDispatcher struct {
	Owner      string
	Generator  AIStageGenerator
	Classifier AIStageClassifier
}

func (d AIStageDispatcher) executor(kind string) (*AIStageExecutor, error) {
	if d.Owner == "" {
		return nil, ErrAIStageUnavailable
	}
	var store AIStageStore
	switch kind {
	case "classify_title":
		store = TitleStageStore{Owner: d.Owner}
	case "extract_vacancy":
		store = ExtractionStageStore{Owner: d.Owner}
	case "assign_roles":
		store = LeafStageStore{Owner: d.Owner}
	default:
		return nil, ErrAIStageUnavailable
	}
	return &AIStageExecutor{
		Store: store, Generator: d.Generator, Classifier: d.Classifier,
		Handoff: &CandidateHandoff{Planner: SQLCandidateHandoffPlanner{Owner: d.Owner}},
	}, nil
}

// Execute не получает задание из очереди и не повторяет платные запросы.
func (d AIStageDispatcher) Execute(ctx context.Context, lease *JobLease) (AIStageOutput, error) {
	if lease == nil {
		return AIStageOutput{}, ErrAIStageUnavailable
	}
	executor, err := d.executor(lease.Job().Type)
	if err != nil {
		return AIStageOutput{}, err
	}
	return executor.Execute(ctx, lease)
}
