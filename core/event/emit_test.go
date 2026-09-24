package event_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-messaging/core/event"
)

var _ event.Tx = (*sql.Tx)(nil)

func TestTxExcludesAPool(t *testing.T) {
	tx := reflect.TypeFor[event.Tx]()
	for _, pool := range []reflect.Type{reflect.TypeFor[*sql.DB](), reflect.TypeFor[*sql.Conn]()} {
		if pool.Implements(tx) {
			t.Errorf("%v satisfies event.Tx; a pool must not", pool)
		}
	}
}
