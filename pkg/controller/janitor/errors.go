package janitor

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// TODO: Perhaps this is useful outside janitor?
type EventBroadcaster interface {
	Broadcast(recorder record.EventRecorder)
}

type RecordableError struct {
	Object      interface{}
	Reason      string
	MessageFmt  string
	MessageArgs []interface{}
	EventType   string
}

func (e RecordableError) Error() string {
	return fmt.Sprintf(e.MessageFmt, e.MessageArgs...)
}

func (e RecordableError) Broadcast(obj runtime.Object, recorder record.EventRecorder) {
	recorder.Eventf(obj, e.EventType, e.Reason, e.MessageFmt, e.MessageArgs...)
}

func NewRecordableError(
	eventType string,
	reason string,
	messageFmt string,
	args ...interface{},
) *RecordableError {
	return &RecordableError{
		Reason:      reason,
		MessageFmt:  messageFmt,
		MessageArgs: args,
		EventType:   eventType,
	}
}
