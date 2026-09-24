package messaging_test

import (
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/messaging"
)

func TestValidate(t *testing.T) {
	if err := (messaging.Subscription{Name: "workers", Types: []string{"a"}}).Validate(); err != nil {
		t.Errorf("valid subscription: %v", err)
	}
	cases := map[string]struct {
		sub  messaging.Subscription
		want string
	}{
		"no name":        {messaging.Subscription{}, "name is required"},
		"dotted name":    {messaging.Subscription{Name: "a.b"}, "must not contain"},
		"wildcard name":  {messaging.Subscription{Name: "a>"}, "must not contain"},
		"spaced name":    {messaging.Subscription{Name: "a b"}, "must not contain"},
		"empty type":     {messaging.Subscription{Name: "a", Types: []string{""}}, "empty type"},
		"negative max":   {messaging.Subscription{Name: "a", MaxDeliver: -1}, "max deliver"},
		"negative wait":  {messaging.Subscription{Name: "a", AckWait: -1}, "ack wait"},
		"negative retry": {messaging.Subscription{Name: "a", RetryDelay: -1}, "retry delay"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := c.sub.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	all := messaging.Subscription{Name: "a"}
	if !all.Matches("anything") {
		t.Error("an empty filter must match every type")
	}
	some := messaging.Subscription{Name: "a", Types: []string{"x", "y"}}
	if !some.Matches("y") || some.Matches("z") {
		t.Error("the filter must match exactly its types")
	}
}
