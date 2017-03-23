package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/grapebaba/fabric-operator/spec"

	"k8s.io/client-go/pkg/api/unversioned"
	kwatch "k8s.io/client-go/pkg/watch"
)

type rawEvent struct {
	Type   kwatch.EventType
	Object json.RawMessage
}

func pollEvent(decoder *json.Decoder) (*Event, *unversioned.Status, error) {
	re := &rawEvent{}
	err := decoder.Decode(re)
	if err != nil {
		if err == io.EOF {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("fail to decode raw event from apiserver (%v)", err)
	}

	if re.Type == kwatch.Error {
		status := &unversioned.Status{}
		err = json.Unmarshal(re.Object, status)
		if err != nil {
			return nil, nil, fmt.Errorf("fail to decode (%s) into unversioned.Status (%v)", re.Object, err)
		}
		return nil, status, nil
	}

	ev := &Event{
		Type:   re.Type,
		Object: &spec.Chain{},
	}
	err = json.Unmarshal(re.Object, ev.Object)
	if err != nil {
		return nil, nil, fmt.Errorf("fail to unmarshal Chain object from data (%s): %v", re.Object, err)
	}
	return ev, nil, nil
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
