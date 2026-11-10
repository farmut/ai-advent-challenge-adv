package proxy

import (
	"agile-consultant/internal/testkit/fakeupstream"
)

// The fake upstream implementation lives in internal/testkit/fakeupstream so
// that both the e2e tests and cmd/fake-upstream reuse it. The aliases below
// keep the historical e2e API unchanged.

// DefaultReply is the fake upstream answer when a case scripts none.
const DefaultReply = fakeupstream.DefaultReply

// Reply is one scripted answer.
type Reply = fakeupstream.Reply

// FakeUpstream is a scripted OpenAI-compatible server.
type FakeUpstream = fakeupstream.FakeUpstream

// NewFakeUpstream starts the server.
func NewFakeUpstream() *FakeUpstream { return fakeupstream.NewFakeUpstream() }
