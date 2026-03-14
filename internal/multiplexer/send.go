package multiplexer

import (
	"fmt"
	"strings"
	"time"
)

const submitKeyEnter = "Enter"

type messageSender interface {
	Type() Type
	SendKeys(session, keys string) error
	SendKeysLiteral(session, keys string) error
	SendText(session, text string) error
}

type bracketedPasteSender interface {
	SendBracketedPaste(session, text string) error
}

func NeedsBracketedPaste(message string) bool {
	return strings.ContainsAny(message, "\r\n")
}

func NormalizeLineEndings(message string) string {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.ReplaceAll(message, "\r", "\n")
	return message
}

func SendMessage(sender messageSender, session, message string, noEnter bool, splitSubmit bool, submitDelay time.Duration) error {
	if NeedsBracketedPaste(message) {
		message = NormalizeLineEndings(message)
		if paster, ok := sender.(bracketedPasteSender); ok {
			if err := paster.SendBracketedPaste(session, message); err != nil {
				return fmt.Errorf("send bracketed paste: %w", err)
			}
		} else if err := sender.SendKeysLiteral(session, message); err != nil {
			return fmt.Errorf("send keys: %w", err)
		}

		if noEnter {
			return nil
		}
		if submitDelay > 0 {
			time.Sleep(submitDelay)
		}
		if err := sender.SendText(session, submitKeyEnter); err != nil {
			return fmt.Errorf("send submit key: %w", err)
		}
		return nil
	}

	if noEnter {
		if err := sender.SendKeysLiteral(session, message); err != nil {
			return fmt.Errorf("send keys: %w", err)
		}
		return nil
	}
	if splitSubmit {
		if err := sender.SendKeysLiteral(session, message); err != nil {
			return fmt.Errorf("send keys: %w", err)
		}
		if submitDelay > 0 {
			time.Sleep(submitDelay)
		}
		if err := sender.SendText(session, submitKeyEnter); err != nil {
			return fmt.Errorf("send submit key: %w", err)
		}
		return nil
	}
	if err := sender.SendKeys(session, message); err != nil {
		return fmt.Errorf("send keys: %w", err)
	}
	return nil
}
