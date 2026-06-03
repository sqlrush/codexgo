package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestMapFsError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantNil  bool
		wantCode int64
		wantMsg  string
	}{
		{name: "nil", err: nil, wantNil: true},
		{
			name:     "invalid input maps to invalid request",
			err:      newInvalidInput("bad input"),
			wantCode: InvalidRequestErrorCode,
			wantMsg:  "bad input",
		},
		{
			name:     "wrapped invalid input still maps to invalid request",
			err:      fmt.Errorf("context: %w", newInvalidInput("nested bad input")),
			wantCode: InvalidRequestErrorCode,
			wantMsg:  "context: nested bad input",
		},
		{
			name:     "not found maps to internal error",
			err:      fs.ErrNotExist,
			wantCode: InternalErrorCode,
			wantMsg:  fs.ErrNotExist.Error(),
		},
		{
			name:     "generic error maps to internal error",
			err:      errors.New("boom"),
			wantCode: InternalErrorCode,
			wantMsg:  "boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MapFsError(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil JSONRPCError")
			}
			if got.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got.Message, tc.wantMsg)
			}
		})
	}
}

func TestJSONRPCErrorImplementsError(t *testing.T) {
	var err error = &JSONRPCError{Code: InternalErrorCode, Message: "x"}
	if err.Error() != "x" {
		t.Fatalf("Error() = %q", err.Error())
	}
}
