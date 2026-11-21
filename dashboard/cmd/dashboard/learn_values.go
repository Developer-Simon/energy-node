package main

import (
	"log"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// learnedPublisher is the part of the MQTT client the learned values need.
type learnedPublisher interface {
	PublishRetained(topic, payload string) error
}

// publishLearned runs after every engine round with the values of that round
// (normally Engine.Values, in the demo with the demo house state). With the habit group off
// nothing is updated, so the retained values are not refreshed and age out on
// the rule side. A message that could not be sent is taken back in the store,
// so the next round sends it again. The store is saved when something changed.
func publishLearned(store *autolearn.LearnedStore, values func() []autolearn.Value, client learnedPublisher, habitOn bool, exists func(string) bool, now time.Time) {
	if !habitOn {
		return
	}
	msgs := store.Update(values(), now, exists)
	if len(msgs) == 0 {
		return
	}
	sendLearned(store, client, msgs)
	if err := store.Save(); err != nil {
		log.Printf("autolearn: learned values not saved: %v", err)
	}
}

// sendLearned publishes the messages and reverts the ones that failed.
func sendLearned(store *autolearn.LearnedStore, client learnedPublisher, msgs []autolearn.Message) {
	var failed []autolearn.Message
	for _, m := range msgs {
		if err := client.PublishRetained(m.Topic, m.Payload); err != nil {
			failed = append(failed, m)
		}
	}
	if len(failed) > 0 {
		store.Revert(failed)
	}
}

// resetLearned deletes the retained learned values after "Gelerntes löschen".
// The store is already empty, so a failed delete is only logged.
func resetLearned(store *autolearn.LearnedStore, client learnedPublisher) {
	for _, m := range store.Reset() {
		if err := client.PublishRetained(m.Topic, m.Payload); err != nil {
			log.Printf("autolearn: learned value %s not cleared: %v", m.Topic, err)
		}
	}
}
