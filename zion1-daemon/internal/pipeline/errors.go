// errors.go — pipeline 에러 분류.
//
// 영구 에러 (Permanent) 와 일시적 에러 (Transient) 를 구분 — 영구는 retry 무의미 +
// dispatcher에서 mark consumed 해야 재시작 시 무한 reprocess 방지.
//
// 핸들러가 명시적으로 PermanentError로 wrap하면 pipeline이 retry skip.
package pipeline

import (
	"errors"
	"fmt"
)

// PermanentError는 retry 해도 결과가 같은 영구 실패.
// 예: ABI 검증 실패, 만료된 nonce, axis 범위 초과, 잘못된 attribute.
// 호출자가 `return Permanent(err)` 또는 `Permanent(...).Wrap(...)`로 사용.
type PermanentError struct {
	Err error
}

func (p *PermanentError) Error() string {
	return "permanent: " + p.Err.Error()
}

func (p *PermanentError) Unwrap() error {
	return p.Err
}

// Permanent wraps err to mark it as permanent. nil → nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// Permanentf is fmt.Errorf + Permanent wrap.
func Permanentf(format string, args ...interface{}) error {
	return &PermanentError{Err: fmt.Errorf(format, args...)}
}

// IsPermanent reports whether err (or any wrapped) is a PermanentError.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}
