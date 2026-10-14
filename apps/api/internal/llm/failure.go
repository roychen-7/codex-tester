package llm

import "errors"

// requestFailure contains only application-owned messages, never provider bodies,
// URLs, credentials or raw transport errors.
type requestFailure struct {
	code    string
	message string
}

func (e *requestFailure) Error() string {
	return e.message
}

func requestError(code, message string) error {
	return &requestFailure{code: code, message: message}
}

// FailureDetails is safe for persisted UI errors and structured diagnostic logs.
func FailureDetails(err error) (code, message string) {
	var failure *requestFailure
	if errors.As(err, &failure) {
		return failure.code, failure.message
	}

	return "generation_failed", "暂时无法生成回复，请重试"
}
