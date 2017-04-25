package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/grapebaba/fabric-operator/spec"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kwatch "k8s.io/apimachinery/pkg/watch"
)

type rawEvent struct {
	Type   kwatch.EventType
	Object json.RawMessage
}

// panicTimer panics when it reaches the given duration.
type panicTimer struct {
	d   time.Duration
	msg string
	t   *time.Timer
}

func newPanicTimer(d time.Duration, msg string) *panicTimer {
	return &panicTimer{
		d:   d,
		msg: msg,
	}
}

func (pt *panicTimer) start() {
	pt.t = time.AfterFunc(pt.d, func() {
		panic(pt.msg)
	})
}

// stop stops the timer and resets the elapsed duration.
func (pt *panicTimer) stop() {
	if pt.t != nil {
		pt.t.Stop()
		pt.t = nil
	}
}
