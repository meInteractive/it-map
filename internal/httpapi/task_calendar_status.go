package httpapi

import "context"

type taskCalendarStatusReader interface {
	CalendarSyncStatus(context.Context, string, int64) (string, error)
}

// Для legacy test stubs сохраняем прежний статус, но работающий Go backend
// сообщает реальное pending, чтобы UI не маскировал неотправленную задачу.
func taskCalendarSyncStatus(ctx context.Context, writer any, user string, id int64) string {
	reader, ok := writer.(taskCalendarStatusReader)
	if !ok {
		return "not_connected"
	}
	status, err := reader.CalendarSyncStatus(ctx, user, id)
	if err != nil {
		return "unknown"
	}
	if status == "" {
		return "not_connected"
	}
	return status
}
