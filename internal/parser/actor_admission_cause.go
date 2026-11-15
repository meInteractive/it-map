package parser

// onlyAdmissionCause отклоняет соединённую ошибку cleanup: её нельзя скрыть
// под видом безопасного ожидания перед повтором платной операции.
func onlyAdmissionCause(err, target error) bool {
	if err == target {
		return true
	}
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !onlyAdmissionCause(cause, target) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyAdmissionCause(wrapped.Unwrap(), target)
	}
	return false
}
