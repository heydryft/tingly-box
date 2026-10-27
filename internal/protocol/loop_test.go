package protocol

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/tingly-dev/tingly-box/internal/constant"
)

// HTTP/3 response writers do not implement the deprecated CloseNotifier.
// A plain recorder deliberately has the same limitation.
func TestRunLoopWithoutCloseNotifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "canceled"}[canceled], func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest("GET", "/stream", nil).WithContext(ctx)
			if canceled {
				cancel()
			}
			called := false
			gone := RunLoop(c, func(out io.Writer) bool {
				called = true
				_, _ = io.WriteString(out, "data: done\n\n")
				return false
			})
			assert.Equal(t, canceled, gone)
			assert.Equal(t, !canceled, called)
			if !canceled {
				assert.Equal(t, "data: done\n\n", w.Body.String())
				assert.True(t, w.Flushed)
			}
		})
	}
}

// closeNotifyRecorder adds CloseNotify to httptest.ResponseRecorder so it can
// back a gin.ResponseWriter driven by RunLoop.
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool { return make(chan bool) }

// TestMarkFirstToken_RecordsOnceEarliestWins verifies the single source of
// truth is idempotent: the first signal wins and later calls never overwrite.
func TestMarkFirstToken_RecordsOnceEarliestWins(t *testing.T) {
	c := &gin.Context{}

	_, exists := c.Get(constant.CtxKeyFirstTokenTime)
	assert.False(t, exists)

	MarkFirstToken(c)
	v, ok := c.Get(constant.CtxKeyFirstTokenTime)
	assert.True(t, ok)
	first := v.(time.Time)

	time.Sleep(5 * time.Millisecond)
	MarkFirstToken(c)
	v2, _ := c.Get(constant.CtxKeyFirstTokenTime)
	assert.Equal(t, first, v2.(time.Time), "later mark must not overwrite earliest signal")
}

// TestCommitFirstChunk_DoesNotRecordFirstToken verifies the commit seam does not
// record TTFT — only the first content token does, marked by each protocol.
func TestCommitFirstChunk_DoesNotRecordFirstToken(t *testing.T) {
	c := &gin.Context{}

	CommitFirstChunk(c)

	_, ok := c.Get(constant.CtxKeyFirstTokenTime)
	assert.False(t, ok, "CommitFirstChunk must not record TTFT; only content events do")
}

// TestRunLoop_DoesNotRecordFirstTokenOnFirstChunk verifies RunLoop does not
// record TTFT on the first chunk; it sees raw bytes, not content events.
func TestRunLoop_DoesNotRecordFirstTokenOnFirstChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := &closeNotifyRecorder{httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)

	_, exists := c.Get(constant.CtxKeyFirstTokenTime)
	assert.False(t, exists, "no first-token time before streaming starts")

	calls := 0
	RunLoop(c, func(wr io.Writer) bool {
		calls++
		if calls == 1 {
			_, _ = wr.Write([]byte("data: hi\n\n"))
			return true // first chunk produced
		}
		return false // stop
	})

	_, ok := c.Get(constant.CtxKeyFirstTokenTime)
	assert.False(t, ok, "RunLoop must not record TTFT on the first byte; handlers mark content events")
}
