package multiplexer

import (
	"testing"
	"time"
)

type sendMessageCall struct {
	session string
	text    string
}

type sendMessageStub struct {
	muxType              Type
	sendKeysCalls        []sendMessageCall
	sendKeysLiteralCalls []sendMessageCall
	sendTextCalls        []sendMessageCall
	sendPasteCalls       []sendMessageCall
}

func (s *sendMessageStub) Type() Type {
	return s.muxType
}

func (s *sendMessageStub) SendKeys(session, keys string) error {
	s.sendKeysCalls = append(s.sendKeysCalls, sendMessageCall{session: session, text: keys})
	return nil
}

func (s *sendMessageStub) SendKeysLiteral(session, keys string) error {
	s.sendKeysLiteralCalls = append(s.sendKeysLiteralCalls, sendMessageCall{session: session, text: keys})
	return nil
}

func (s *sendMessageStub) SendText(session, text string) error {
	s.sendTextCalls = append(s.sendTextCalls, sendMessageCall{session: session, text: text})
	return nil
}

func (s *sendMessageStub) SendBracketedPaste(session, text string) error {
	s.sendPasteCalls = append(s.sendPasteCalls, sendMessageCall{session: session, text: text})
	return nil
}

func TestSendMessageSingleLineUsesSendKeys(t *testing.T) {
	stub := &sendMessageStub{muxType: TypeTmux}

	if err := SendMessage(stub, "sess", "hello", false, false, 0); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if len(stub.sendKeysCalls) != 1 {
		t.Fatalf("SendKeys calls = %d, want 1", len(stub.sendKeysCalls))
	}
	if got := stub.sendKeysCalls[0]; got.session != "sess" || got.text != "hello" {
		t.Fatalf("SendKeys call = %+v", got)
	}
	if len(stub.sendPasteCalls) != 0 || len(stub.sendKeysLiteralCalls) != 0 || len(stub.sendTextCalls) != 0 {
		t.Fatalf("unexpected extra calls: paste=%d literal=%d text=%d", len(stub.sendPasteCalls), len(stub.sendKeysLiteralCalls), len(stub.sendTextCalls))
	}
}

func TestSendMessageMultilineUsesBracketedPasteAndSubmit(t *testing.T) {
	stub := &sendMessageStub{muxType: TypeTmux}

	if err := SendMessage(stub, "sess", "line one\r\nline two", false, false, 0); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if len(stub.sendPasteCalls) != 1 {
		t.Fatalf("SendBracketedPaste calls = %d, want 1", len(stub.sendPasteCalls))
	}
	if got := stub.sendPasteCalls[0]; got.session != "sess" || got.text != "line one\nline two" {
		t.Fatalf("SendBracketedPaste call = %+v", got)
	}
	if len(stub.sendTextCalls) != 1 || stub.sendTextCalls[0].text != submitKeyEnter {
		t.Fatalf("SendText calls = %+v, want Enter", stub.sendTextCalls)
	}
	if len(stub.sendKeysCalls) != 0 || len(stub.sendKeysLiteralCalls) != 0 {
		t.Fatalf("unexpected key calls: SendKeys=%d SendKeysLiteral=%d", len(stub.sendKeysCalls), len(stub.sendKeysLiteralCalls))
	}
}

func TestSendMessageMultilineNoEnterUsesBracketedPasteOnly(t *testing.T) {
	stub := &sendMessageStub{muxType: TypeZellij}

	if err := SendMessage(stub, "sess", "line one\nline two", true, false, 0); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if len(stub.sendPasteCalls) != 1 {
		t.Fatalf("SendBracketedPaste calls = %d, want 1", len(stub.sendPasteCalls))
	}
	if len(stub.sendTextCalls) != 0 {
		t.Fatalf("SendText calls = %d, want 0", len(stub.sendTextCalls))
	}
}

func TestSendMessageSingleLineDelayUsesLiteralThenEnter(t *testing.T) {
	stub := &sendMessageStub{muxType: TypeTmux}

	if err := SendMessage(stub, "sess", "hello", false, true, time.Nanosecond); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if len(stub.sendKeysLiteralCalls) != 1 || stub.sendKeysLiteralCalls[0].text != "hello" {
		t.Fatalf("SendKeysLiteral calls = %+v, want hello", stub.sendKeysLiteralCalls)
	}
	if len(stub.sendTextCalls) != 1 || stub.sendTextCalls[0].text != submitKeyEnter {
		t.Fatalf("SendText calls = %+v, want Enter", stub.sendTextCalls)
	}
	if len(stub.sendPasteCalls) != 0 || len(stub.sendKeysCalls) != 0 {
		t.Fatalf("unexpected calls: paste=%d keys=%d", len(stub.sendPasteCalls), len(stub.sendKeysCalls))
	}
}
