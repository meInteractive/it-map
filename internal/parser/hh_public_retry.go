package parser

import "itmap/internal/integrations/hhpublic"

// Ошибка освобождения lease, объединённая с сетевой, не должна маскироваться
// автоматическим retry. Допускаем только обёртки без других причин.
func onlyTemporaryPublicHH(err error) bool {
	if err == hhpublic.ErrTemporary {
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
			if !onlyTemporaryPublicHH(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyTemporaryPublicHH(wrapped.Unwrap())
	}
	return false
}
