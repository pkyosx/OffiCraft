// Skeleton generated from server/ocserverd/auth_alert.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNoteFactorRefusedAfterCorrectPassword(t *testing.T) {
	t.Skip("TODO: noteFactorRefusedAfterCorrectPassword records one 「password correct, second factor wrong」 login refusal and, at most once per authAlertInterval, hands the accumulated count to a goroutine that tells the assistant.")
}

func TestDispatchAuthAlert(t *testing.T) {
	t.Skip("TODO: dispatchAuthAlert is the one indirection in this file, and it exists so the asynchrony above is FALSIFIABLE rather than merely visible: a test can install a deliverer that blocks for seconds and assert the caller still returned immediately.")
}

func TestDeliverPasswordExposedAlert(t *testing.T) {
	t.Skip("TODO: deliverPasswordExposedAlert writes the durable chat row and fans it, exactly the way a scheduled message is delivered (scheduled_message.go): a row the recipient owns whether or not anything is connected right now, plus the convenience delta for whatever is.")
}

func TestPasswordExposedAlertBody(t *testing.T) {
	t.Skip("TODO: passwordExposedAlertBody is the sentence the assistant reads.")
}
